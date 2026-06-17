package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/themisto/backend/internal/store"
	"github.com/themisto/backend/internal/token"
)

func main() {
	dbURL := os.Getenv("DB_DSN")
	if dbURL == "" {
		panic("DB_DSN not set")
	}

	st, err := store.New(dbURL)
	if err != nil {
		panic(err)
	}

	orgID := "a0000000-0000-0000-0000-000000000001"

	device, err := st.CreateDevice(context.Background(), orgID, "Demo-MacBook-4", "darwin", "1.0.0")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created Device ID: %s\n", device.ID)

	rawToken, tokenHash, err := token.Generate()
	if err != nil {
		panic(err)
	}

	_, err = st.CreateEnrollmentToken(context.Background(), device.ID, tokenHash, time.Now().Add(24*time.Hour))
	if err != nil {
		panic(err)
	}
	fmt.Printf("\n--- COPY THIS TOKEN ---\n\nDEVICE_ID=%s\nRAW_TOKEN=%s\n\n-----------------------\n", device.ID, rawToken)
}
