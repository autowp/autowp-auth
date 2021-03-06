package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/autowp/auth/oauth2server"
	"github.com/autowp/auth/oauth2server/errors"

	"github.com/autowp/auth/oauth2server/generates"

	"github.com/autowp/auth/oauth2server/server"

	"github.com/autowp/auth/oauth2server/store"

	"github.com/autowp/auth/oauth2server/manage"

	"github.com/dgrijalva/jwt-go"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/facebook"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/vk"

	goauth2 "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"

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
	stateMap    *StateMap
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
		usersDB:     usersDB,
		oauthServer: oauthServer,
		Loc:         loc,
		waitGroup:   wg,
		stateMap:    NewStateMap(time.Hour),
	}

	oauthServer.SetSocialAuthorizationHandler(func(code, stateID, remoteAddr string) (int64, string, error) {

		if stateID == "" {
			return 0, "", errors.ErrInvalidRequest
		}

		state := s.stateMap.Get(stateID)
		if state == nil {
			return 0, "", errors.ErrInvalidRequest
		}

		var userID int64

		userInfo := UserInfo{}

		switch state.Service {
		case Google:

			var config = &oauth2.Config{
				ClientID:     s.config.Services.Google.ClientID,
				ClientSecret: s.config.Services.Google.ClientSecret,
				Endpoint:     google.Endpoint,
				Scopes:       s.config.Services.Google.Scopes,
				RedirectURL:  s.config.Services.RedirectURI,
			}
			token, err := config.Exchange(context.Background(), code)
			if err != nil {
				return 0, "", err
			}

			httpClient := config.Client(context.Background(), token)

			goauth2Service, err := goauth2.NewService(context.Background(), option.WithHTTPClient(httpClient))
			if err != nil {
				return 0, "", err
			}

			gUserInfo, err := goauth2Service.Userinfo.V2.Me.Get().Do()
			if err != nil {
				return 0, "", err
			}

			userInfo.ID = gUserInfo.Id
			userInfo.Name = gUserInfo.Name
			userInfo.URL = gUserInfo.Link

		case Facebook:
			var config = &oauth2.Config{
				ClientID:     s.config.Services.Facebook.ClientID,
				ClientSecret: s.config.Services.Facebook.ClientSecret,
				Endpoint:     facebook.Endpoint,
				Scopes:       s.config.Services.Facebook.Scopes,
				RedirectURL:  s.config.Services.RedirectURI,
			}
			token, err := config.Exchange(context.Background(), code)
			if err != nil {
				return 0, "", err
			}

			httpClient := config.Client(context.Background(), token)

			resp, err := httpClient.Get("https://graph.facebook.com/v6.0/me?fields=id,name")
			if err != nil {
				return 0, "", err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return 0, "", fmt.Errorf("Unexpected status code %d", resp.StatusCode)
			}

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return 0, "", err
			}

			fbUser := FacebookUser{}
			err = json.Unmarshal(body, &fbUser)
			if err != nil {
				return 0, "", err
			}

			userInfo.ID = fbUser.ID
			userInfo.Name = fbUser.Name
			userInfo.URL = ""

		case VK:
			var config = &oauth2.Config{
				ClientID:     s.config.Services.VK.ClientID,
				ClientSecret: s.config.Services.VK.ClientSecret,
				Endpoint:     vk.Endpoint,
				Scopes:       s.config.Services.VK.Scopes,
				RedirectURL:  s.config.Services.RedirectURI,
			}
			token, err := config.Exchange(context.Background(), code)
			if err != nil {
				return 0, "", err
			}

			url, err := url.Parse("https://api.vk.com/method/users.get")
			if err != nil {
				return 0, "", err
			}

			q := url.Query()
			q.Set("fields", "id,first_name,last_name,screen_name")
			q.Set("v", "5.103")
			q.Set("lang", state.Language)
			q.Set("access_token", token.AccessToken)
			url.RawQuery = q.Encode()

			resp, err := http.Get(url.String())
			if err != nil {
				return 0, "", err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return 0, "", fmt.Errorf("Unexpected status code %d", resp.StatusCode)
			}

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return 0, "", err
			}

			vkUsers := VKGetUsers{}
			err = json.Unmarshal(body, &vkUsers)
			if err != nil {
				return 0, "", err
			}

			if len(vkUsers.Response) <= 0 {
				return 0, "", fmt.Errorf("Empty response")
			}

			vkUser := vkUsers.Response[0]

			userInfo.ID = strconv.FormatInt(vkUser.ID, 10)
			userInfo.Name = strings.TrimSpace(vkUser.FirstName + " " + vkUser.LastName)
			userInfo.URL = "http://vk.com/" + vkUser.ScreenName

		default:
			return 0, "", fmt.Errorf("Unexpected service %s", state.Service)
		}

		if userInfo.ID == "" {
			return 0, "", fmt.Errorf("Failed to get user id")
		}

		if userInfo.Name == "" {
			return 0, "", fmt.Errorf("Failed to get user name")
		}

		userID, err = s.registerUser(&userInfo, state, "Europe/Moscow", remoteAddr)
		if err != nil {
			return 0, "", err
		}

		s.stateMap.Delete(stateID)

		return userID, state.RedirectURI, nil
	})

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

	srv := server.NewServer(&server.Config{}, manager)

	srv.SetPasswordAuthorizationHandler(func(username, password string) (userID int64, err error) {
		user, err := userStore.GetUserByCredentials(username, password)
		if err != nil {
			return 0, err
		}
		if user != nil {
			return user.ID, nil
		}
		return 0, nil
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

func randomBase64String(l int) (string, error) {
	buff := make([]byte, int(math.Round(float64(l)/float64(1.33333333333))))
	_, err := rand.Read(buff)
	if err != nil {
		return "", err
	}
	str := base64.RawURLEncoding.EncodeToString(buff)
	return str[:l], nil // strip 1 extra character we get from odd length results
}

func (s *Service) getUserIDFromRequest(c *gin.Context) (int64, error) {
	authorizationHeader := c.GetHeader("Authorization")

	if authorizationHeader == "" {
		return 0, nil
	}

	bearerToken := strings.Split(authorizationHeader, " ")

	if len(bearerToken) != 2 || bearerToken[0] != "Bearer" {
		return 0, fmt.Errorf("Invalid authorization token")
	}

	token, err := jwt.Parse(bearerToken[1], func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("There was an error")
		}
		return []byte(s.config.OAuth.Secret), nil
	})
	if err != nil {
		return 0, err
	}

	if !token.Valid {
		return 0, fmt.Errorf("Invalid authorization token")
	}

	var claims jwt.StandardClaims
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "json",
		Result:  &claims,
	})
	if err != nil {
		return 0, err
	}
	err = decoder.Decode(token.Claims)
	if err != nil {
		return 0, err
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, err
	}

	return userID, nil
}

func (s *Service) setupRouter() {
	r := gin.New()
	r.Use(gin.Recovery())

	apiGroup := r.Group("/api/oauth")
	{
		/*apiGroup.GET("/authorize", func(c *gin.Context) {
			err := s.oauthServer.HandleAuthorizeRequest(c.Writer, c.Request)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
			}
		})*/

		apiGroup.POST("/token", func(c *gin.Context) {

			client := s.config.OAuth.Clients[0]

			trd := oauth2server.TokenRequestData{}

			err := c.ShouldBind(&trd)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
			}

			trd.ClientID = client.GetID()
			trd.ClientSecret = client.GetSecret()

			gt, tgr, _, err := s.oauthServer.ValidationTokenRequest(c, &trd)
			if err != nil {
				s.oauthServer.TokenError(c, err)
				return
			}

			ti, err := s.oauthServer.GetAccessToken(gt, tgr)
			if err != nil {
				s.oauthServer.TokenError(c, err)
				return
			}

			s.oauthServer.Token(c, s.oauthServer.GetTokenData(ti), nil, 0)
		})

		apiGroup.GET("/service", func(c *gin.Context) {

			userID, err := s.getUserIDFromRequest(c)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
				return
			}

			language := s.config.Hosts[0].Language
			for _, host := range s.config.Hosts {
				if host.Hostname == c.Request.Host {
					language = host.Language
				}
			}

			redirectURI := c.Query("redirect_uri")
			if redirectURI == "" {
				c.String(http.StatusBadRequest, "invalid redirect_uri")
				return
			}

			serviceName := ExternalService(c.Query("service"))

			if serviceName == "" {
				c.String(http.StatusBadRequest, "unexpected service")
				return
			}

			stateID, err := randomBase64String(32)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			state := State{
				UserID:      userID,
				Language:    language,
				Service:     serviceName,
				RedirectURI: redirectURI,
			}

			s.stateMap.Put(stateID, state)

			var config *oauth2.Config

			switch serviceName {
			case Google:
				config = &oauth2.Config{
					ClientID:     s.config.Services.Google.ClientID,
					ClientSecret: s.config.Services.Google.ClientSecret,
					Endpoint:     google.Endpoint,
					Scopes:       s.config.Services.Google.Scopes,
					RedirectURL:  s.config.Services.RedirectURI,
				}
			case Facebook:
				config = &oauth2.Config{
					ClientID:     s.config.Services.Facebook.ClientID,
					ClientSecret: s.config.Services.Facebook.ClientSecret,
					Endpoint:     facebook.Endpoint,
					Scopes:       s.config.Services.Facebook.Scopes,
					RedirectURL:  s.config.Services.RedirectURI,
				}
			case VK:
				config = &oauth2.Config{
					ClientID:     s.config.Services.VK.ClientID,
					ClientSecret: s.config.Services.VK.ClientSecret,
					Endpoint:     vk.Endpoint,
					Scopes:       s.config.Services.VK.Scopes,
					RedirectURL:  s.config.Services.RedirectURI,
				}
			default:
				c.Status(http.StatusNotFound)
			}

			c.JSON(http.StatusOK, gin.H{
				"url": config.AuthCodeURL(stateID, oauth2.AccessTypeOnline),
			})
		})

		apiGroup.GET("/service-callback", func(c *gin.Context) {
			client := s.config.OAuth.Clients[0]

			trd := oauth2server.TokenRequestData{
				ClientID:     client.GetID(),
				ClientSecret: client.GetSecret(),
				GrantType:    oauth2server.SocialAuthorizationCode.String(),
				State:        c.Query("state"),
				Code:         c.Query("code"),
				Scope:        c.Query("scope"),
				ClientIP:     c.ClientIP(),
			}

			gt, tgr, redirectURI, err := s.oauthServer.ValidationTokenRequest(c, &trd)
			if err != nil {
				s.oauthServer.TokenError(c, err)
				return
			}

			ti, err := s.oauthServer.GetAccessToken(gt, tgr)
			if err != nil {
				s.oauthServer.TokenError(c, err)
				return
			}

			td := s.oauthServer.GetTokenData(ti)

			encoded, err := json.Marshal(td)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			c.Header("Content-Type", "application/json;charset=UTF-8")
			c.Header("Cache-Control", "no-store")
			c.Header("Pragma", "no-cache")

			u, err := url.Parse(redirectURI)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			q, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			q.Add("token", string(encoded))
			u.RawQuery = q.Encode()

			c.Redirect(http.StatusFound, u.String())
		})
	}

	s.router = r
}

func (s *Service) registerUser(userInfo *UserInfo, state *State, timezone string, ip string) (int64, error) {

	stateUserID := state.UserID

	if stateUserID <= 0 {
		row := s.usersDB.QueryRow("SELECT user_id FROM user_account WHERE service_id = ? AND external_id = ?", state.Service, userInfo.ID)

		err := row.Scan(&stateUserID)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
	}

	if stateUserID <= 0 {

		res, err := s.usersDB.Exec(`
			INSERT INTO users (login, e_mail, password, email_to_check, hide_e_mail, email_check_code, name, reg_date, last_online, timezone, last_ip, language) 
			VALUES (NULL, NULL, '', NULL, 1, NULL, ?, NOW(), NOW(), ?, INET6_ATON(?), ?)
		`,
			userInfo.Name,
			timezone,
			ip,
			state.Language,
		)
		if err != nil {
			return 0, err
		}

		stateUserID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	}

	if stateUserID <= 0 {
		return 0, fmt.Errorf("Account not found")
	}

	_, err := s.usersDB.Exec(`
		INSERT INTO user_account (service_id, external_id, user_id, used_for_reg, name, link) 
		VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			user_id = VALUES(user_id),
			name = VALUES(name),
			link = VALUES(link)
	`,
		state.Service,
		userInfo.ID,
		stateUserID,
		state.UserID == 0,
		userInfo.Name,
		userInfo.URL,
	)
	if err != nil {
		return 0, err
	}

	return stateUserID, nil
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
