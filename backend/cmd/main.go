package main

import (
	"fmt"
	"net/http"

	"github.com/shinyhawk/Quiz/app/auth"
	"github.com/shinyhawk/Quiz/app/session"
	"github.com/shinyhawk/Quiz/app/user"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/shinyhawk/Quiz/api"
	"github.com/shinyhawk/Quiz/app/game"
	"github.com/shinyhawk/Quiz/config"
	"github.com/shinyhawk/Quiz/repo"
)

func main() {
	cfg := config.GetConfig()

	// Google Config
	gcfg := &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%s/auth/google", cfg.Frontend.Base),
		ClientID:     cfg.Google.ClientId,
		ClientSecret: cfg.Google.ClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	// Repository layer
	db := repo.New(cfg)
	gameRepo := repo.NewGameStore(db)
	userRepo := repo.NewUserStore(db)
	sessionRepo := repo.NewSessionStore(db)

	// Business logic layer
	gameService := game.NewGameService(gameRepo)
	userService := user.NewUserService(userRepo)
	authService := auth.NewAuthService(cfg, gcfg, userService, &http.Client{})
	sessionService := session.NewSessionService(sessionRepo)

	// Presentation layer
	r := api.InitWeb(cfg, gcfg, gameService, authService, userService, sessionService)

	r.Run(fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))
}
