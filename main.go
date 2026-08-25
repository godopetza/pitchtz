package main

import (
	"log"
	"os"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/server"
)

func main() {
	initializers.LoadEnv()
	if initializers.DatabaseDSN() != "" {
		if err := initializers.ConnectToDB(); err != nil {
			log.Fatalf("database connect: %v", err)
		}
		if err := initializers.SyncDatabase(); err != nil {
			log.Fatalf("database migrate: %v", err)
		}
	} else {
		log.Print("DB and DATABASE_URL are not set; using the in-memory store")
	}

	router := server.NewRouter()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("server run: %v", err)
	}
}
