package main

import (
	"context"
	"log"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrate(ctx, db); err != nil {
		return err
	}

	r := newRouter(db, cfg)
	addr := "0.0.0.0:" + cfg.Port
	log.Printf("listening on %s", addr)
	return r.Run(addr)
}
