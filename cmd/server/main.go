package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"

	"tablescore-api/internal/bgg"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

func main() {
	loadDotEnv(".env")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	database, err := store.OpenPostgres(databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	api := httpapi.New(database, bgg.NewFromEnvironment())
	server := &http.Server{Addr: ":" + port, Handler: api.Handler()}
	log.Printf("TableScore API listening on http://localhost:%s", port)
	log.Fatal(server.ListenAndServe())
}

func loadDotEnv(fileName string) {
	file, err := os.Open(fileName)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if _, exists := os.LookupEnv(strings.TrimSpace(key)); !exists {
			_ = os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"`))
		}
	}
}
