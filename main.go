package main

import (
	"log"
	"os"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/server"
	"github.com/godopetza/pitchtz/services"
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
		initializers.SeedCities()
		created, err := initializers.BootstrapAdmin()
		if err != nil {
			log.Fatalf("bootstrap admin: %v", err)
		}
		if created {
			log.Print("bootstrap super admin created; remove BOOTSTRAP_ADMIN_PASSWORD from the environment")
		}
		ownerCreated, err := initializers.BootstrapOwner()
		if err != nil {
			log.Fatalf("bootstrap owner: %v", err)
		}
		if ownerCreated {
			log.Print("bootstrap test owner created with a demo venue and pitch; remove BOOTSTRAP_OWNER_PASSWORD from the environment")
		}
	} else {
		log.Print("DB and DATABASE_URL are not set; using the in-memory store")
	}

	services.StartHoldSweeper()

	router := server.NewRouter()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("server run: %v", err)
	}
}
