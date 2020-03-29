package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"gopkg.in/oauth2.v3/errors"
	"gopkg.in/oauth2.v3/generates"
	"gopkg.in/oauth2.v3/manage"
	"gopkg.in/oauth2.v3/server"
	"gopkg.in/oauth2.v3/store"

	_ "github.com/go-sql-driver/mysql" // enable mysql driver
	_ "github.com/jackc/pgx/v4/stdlib" // postgresql driver

	"github.com/golang-migrate/migrate"
	_ "github.com/golang-migrate/migrate/database/postgres" // enable pgsql migrations
	_ "github.com/golang-migrate/migrate/source/file"       // enable file migration source
)

// Service Main Object
type Service struct {
	config      Config
	db          *sql.DB
	usersDB     *sql.DB
	oauthServer *server.Server
	Loc         *time.Location
	waitGroup   *sync.WaitGroup
	httpServer  *http.Server
	router      *gin.Engine
	logger      *log.Logger
}

// NewService constructor
func NewService(wg *sync.WaitGroup, config Config) (*Service, error) {

	var err error

	loc, err := time.LoadLocation("UTC")
	if err != nil {
		return nil, err
	}

	db, err := connectDb(config.OAuth.Driver, config.OAuth.DSN)
	if err != nil {
		fmt.Println(err)
		sentry.CaptureException(err)
		return nil, err
	}

	usersDB, err := connectDb(config.OAuth.UserStore.Driver, config.OAuth.UserStore.DSN)
	if err != nil {
		fmt.Println(err)
		sentry.CaptureException(err)
		return nil, err
	}

	err = applyMigrations(config.Migrations)
	if err != nil && err != migrate.ErrNoChange {
		fmt.Println(err)
		sentry.CaptureException(err)
		return nil, err
	}

	userStore := NewUserStore(usersDB, config.OAuth.UserStore)

	oauthServer := initOAuthServer(db, userStore, config.OAuth)
	// defer tokenStore.Close()

	s := &Service{
		config:      config,
		db:          db,
		oauthServer: oauthServer,
		Loc:         loc,
		waitGroup:   wg,
	}

	s.setupRouter()

	s.ListenHTTP()

	return s, nil
}

func connectDb(driverName string, dsn string) (*sql.DB, error) {
	start := time.Now()
	timeout := 60 * time.Second

	log.Println("Waiting for database via " + driverName + ": " + dsn)

	var db *sql.DB
	var err error
	for {
		db, err = sql.Open(driverName, dsn)
		if err != nil {
			return nil, err
		}

		err = db.Ping()
		if err == nil {
			log.Println("Started.")
			break
		}

		if time.Since(start) > timeout {
			return nil, err
		}

		fmt.Print(".")
		fmt.Println(err)
		time.Sleep(100 * time.Millisecond)
	}

	return db, nil
}

func initOAuthServer(db *sql.DB, userStore *UserStore, config OAuthConfig) *server.Server {
	manager := manage.NewManager()
	manager.SetPasswordTokenCfg(&manage.Config{
		AccessTokenExp:    time.Duration(config.AccessTokenExpiresIn) * time.Minute,
		RefreshTokenExp:   time.Duration(config.RefreshTokenExpiresIn) * time.Minute,
		IsGenerateRefresh: true,
	})
	// default implementation
	//manager.MapAuthorizeGenerate(generates.NewAuthorizeGenerate())
	manager.MapAccessGenerate(
		&generates.JWTAccessGenerate{
			SignedKey:    []byte(config.Secret),
			SignedMethod: jwt.SigningMethodHS512,
		},
	)

	// token store
	tokenStore, err := NewTokenStore(db, WithTokenStoreGCInterval(time.Minute))
	manager.MustTokenStorage(tokenStore, err)

	// client store
	clientStore := store.NewClientStore()
	for _, client := range config.Clients {
		err := clientStore.Set(client.ID, &client)
		if err != nil {
			panic(err)
		}
	}

	manager.MapClientStorage(clientStore)

	srv := server.NewServer(&server.Config{
		TokenType:            config.TokenType,
		AllowedResponseTypes: config.AllowedResponseTypes,
		AllowedGrantTypes:    config.AllowedGrantTypes,
	}, manager)
	srv.SetAllowGetAccessRequest(true)
	srv.SetClientInfoHandler(server.ClientFormHandler)

	srv.SetPasswordAuthorizationHandler(func(username, password string) (userID string, err error) {
		user, err := userStore.GetUserByCredentials(username, password)
		if err != nil {
			return "", err
		}
		if user != nil {
			return strconv.Itoa(user.ID), nil
		}
		return "", nil
	})

	srv.SetInternalErrorHandler(func(err error) (re *errors.Response) {
		log.Println("Internal Error:", err.Error())
		return
	})

	srv.SetResponseErrorHandler(func(re *errors.Response) {
		log.Println("Response Error:", re.Error.Error())
	})

	return srv
}

func (s *Service) setupRouter() {
	r := gin.New()
	r.Use(gin.Recovery())

	apiGroup := r.Group("/oauth")
	{
		/*apiGroup.GET("/authorize", func(c *gin.Context) {
			err := s.oauthServer.HandleAuthorizeRequest(c.Writer, c.Request)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
			}
		})*/

		apiGroup.GET("/token", func(c *gin.Context) {

			values, _ := url.ParseQuery(c.Request.URL.RawQuery)
			client := s.config.OAuth.Clients[0]
			values.Set("client_id", client.GetID())
			values.Set("client_secret", client.GetSecret())

			c.Request.URL.RawQuery = values.Encode()

			err := s.oauthServer.HandleTokenRequest(c.Writer, c.Request)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
			}
		})
	}

	s.router = r
}

// ListenHTTP HTTP thread
func (s *Service) ListenHTTP() {

	s.httpServer = &http.Server{Addr: s.config.Listen, Handler: s.router}

	s.waitGroup.Add(1)
	go func() {
		defer s.waitGroup.Done()
		log.Println("HTTP listener started")

		err := s.httpServer.ListenAndServe()
		if err != nil {
			// cannot panic, because this probably is an intentional close
			log.Printf("Httpserver: ListenAndServe() error: %s", err)
		}

		log.Println("HTTP listener stopped")
	}()
}

func applyMigrations(config MigrationsConfig) error {
	log.Println("Apply migrations")

	dir := config.Dir
	if dir == "" {
		ex, err := os.Executable()
		if err != nil {
			return err
		}
		exPath := filepath.Dir(ex)
		dir = exPath + "/migrations"
	}

	m, err := migrate.New("file://"+dir, config.DSN)
	if err != nil {
		return err
	}

	err = m.Up()
	if err != nil {
		return err
	}
	log.Println("Migrations applied")

	return nil
}

// Close Destructor
func (s *Service) Close() {
	if s.httpServer != nil {
		err := s.httpServer.Shutdown(context.TODO())
		if err != nil {
			panic(err) // failure/timeout shutting down the server gracefully
		}
	}

	s.waitGroup.Wait()

	if s.db != nil {
		err := s.db.Close()
		if err != nil {
			s.logger.Println(err)
		}
	}

	if s.usersDB != nil {
		err := s.usersDB.Close()
		if err != nil {
			s.logger.Println(err)
		}
	}
}
