package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// handleRegister 新規ユーザー登録 (POST /api/auth/register)
func handleRegister(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		// バリデーション（簡易）
		if req.Name == "" || req.Email == "" || req.Password == "" {
			respondWithError(w, http.StatusBadRequest, "全ての項目を入力してください")
			return
		}

		// 💡 ハッカソン最速突破のため、パスワードのハッシュ化は一旦スキップしプレーンテキストで保存
		// （必要であれば後から bcrypt 等を導入）
		passwordHash := req.Password

		// ユニークなユーザーIDの生成 (VARCHAR(26) に合わせてUSR+ナノ秒)
		userID := fmt.Sprintf("USR%d", time.Now().UnixNano())

		// DBへの挿入
		query := "INSERT INTO users (id, name, email, password_hash) VALUES (?, ?, ?, ?)"
		_, err := db.Exec(query, userID, req.Name, req.Email, passwordHash)
		if err != nil {
			log.Printf("ユーザー登録エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "このメールアドレスは既に登録されている可能性があります")
			return
		}

		// レスポンスの返却
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(AuthResponse{
			Token:  userID, // ハッカソン用の簡易セッションとしてIDをそのままトークン代わりにする
			UserID: userID,
			Name:   req.Name,
		})
	}
}

// handleLogin ログイン (POST /api/auth/login)
func handleLogin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		var user User
		query := "SELECT id, name, password_hash FROM users WHERE email = ?"
		err := db.QueryRow(query, req.Email).Scan(&user.ID, &user.Name, &user.PasswordHash)
		if err == sql.ErrNoRows || user.PasswordHash != req.Password {
			respondWithError(w, http.StatusUnauthorized, "メールアドレスまたはパスワードが間違っています")
			return
		} else if err != nil {
			log.Printf("ログインエラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AuthResponse{
			Token:  user.ID,
			UserID: user.ID,
			Name:   user.Name,
		})
	}
}

// respondWithError エラーレスポンスを綺麗に返すための共通ヘルパー関数
func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
