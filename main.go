package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
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

	// 疎通確認用のAPIエンドポイント
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		// 実際にDBにPingを打って接続確認
		//err := db.Ping()
		//if err != nil {
			//log.Printf("DB接続失敗: %v", err)
			//http.Error(w, "DB Connection Failed", http.StatusInternalServerError)
			//return
		}

		// CORS設定（フロントエンドのVercelから呼び出せるようにする準備）
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, "DB Connection Success! Ready to Hack!")
	})

	// Cloud Runが指定するポート（デフォルト8080）でサーバー起動
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
