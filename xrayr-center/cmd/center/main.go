package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/XrayR-project/XrayR/xrayr-center/internal/config"
	"github.com/XrayR-project/XrayR/xrayr-center/internal/db"
	"github.com/XrayR-project/XrayR/xrayr-center/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatal("migrate: ", err)
	}
	if err := seedAdmin(ctx, pool); err != nil {
		log.Fatal("seed: ", err)
	}
	srv := httpapi.New(cfg, pool)
	log.Printf("listening %s", cfg.ListenAddr)
	go func() {
		if err := http.ListenAndServe(cfg.ListenAddr, srv.Router()); err != nil {
			log.Fatal(err)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

func seedAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO admin_user(username, password_hash) VALUES ('admin', $1)`, string(hash))
	if err != nil {
		return err
	}
	log.Println("seeded default admin user: admin / admin123 (请立即修改密码)")
	return nil
}
