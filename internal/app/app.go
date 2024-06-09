package app

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
)

func Run(ctx context.Context, server *fiber.App) {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM)

	go func() {
		err := server.Listen(":8080")

		if err != nil {
			log.Errorf("error starting server: %v", err)
		}
		log.Info("server stopped")
		stop()
	}()

	<-ctx.Done()
	log.Info("shutting down server")
}
