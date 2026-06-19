package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}
func main() {
	// DB接続のための準備（Cloud Runの環境変数から取得）
	mysqlUser := os.Getenv("MYSQL_USER")
	mysqlPwd := os.Getenv("MYSQL_PWD")
	mysqlHost := os.Getenv("MYSQL_HOST")
	mysqlDatabase := os.Getenv("MYSQL_DATABASE")

	// 接続文字列の作成
	connStr := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true", mysqlUser, mysqlPwd, mysqlHost, mysqlDatabase)
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		log.Fatalf("DB構成エラー: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/api/health", withCORS(func(w http.ResponseWriter, r *http.Request) {
		err := db.Ping()
		if err != nil {
			log.Printf("DB接続失敗: %v", err)
			http.Error(w, "DB Connection Failed", http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "DB Connection Success! Ready to Hack!")
	}))

	http.HandleFunc("/api/auth/register", withCORS(handleRegister(db)))
	http.HandleFunc("/api/auth/login", withCORS(handleLogin(db)))

	// Cloud Runが指定するポート（デフォルト8080）でサーバー起動
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
