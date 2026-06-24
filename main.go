package main

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	// _ "github.com/google/generative-ai-go/genai" // main.goで使っていない場合は消してOK
	"github.com/rs/cors"
	// _ "google.golang.org/api/option" // main.goで使っていない場合は消してOK
	"log"
	"net/http"
	"os"
)

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

	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		err := db.Ping()
		if err != nil {
			log.Printf("DB接続失敗: %v", err)
			http.Error(w, "DB Connection Failed", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "DB Connection Success! Ready to Hack!")
	})

	http.HandleFunc("/api/auth/register", handleRegister(db))
	http.HandleFunc("/api/auth/login", handleLogin(db))

	http.HandleFunc("/api/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetItems(db)(w, r)
		case http.MethodPost:
			handleCreateItem(db)(w, r)
		default:
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		}
	})

	http.HandleFunc("/api/items/ai-suggest", handleAISuggest(db))

	http.HandleFunc("/api/chat/rooms", handleCreateRoom(db))
	http.HandleFunc("/api/chat/rooms/messages", handleRoomMessages(db)) // ※もしフロントエンド側が /api/chat/messages などを呼んでいる場合は、そちらに合わせてください！

	c := cors.AllowAll()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("サーバーをポート %s で起動します...", port)

	// c.Handler(http.DefaultServeMux) でサーバー全体をCORS許可で包み込む！
	err = http.ListenAndServe(":"+port, c.Handler(http.DefaultServeMux))

	if err != nil {
		log.Fatal("サーバー起動エラー:", err)
	}
}
