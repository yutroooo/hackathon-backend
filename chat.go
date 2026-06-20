package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// handleCreateRoom チャット部屋の作成 (POST /api/rooms)
func handleCreateRoom(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req CreateRoomRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		if req.ItemID == "" || req.Type == "" {
			respondWithError(w, http.StatusBadRequest, "必須項目(item_id, type)が不足しています")
			return
		}

		// ユニークな部屋IDの生成 (RM+ナノ秒)
		roomID := fmt.Sprintf("RM%d", time.Now().UnixNano())

		// DBに部屋を挿入
		query := "INSERT INTO chat_rooms (id, item_id, type, status) VALUES (?, ?, ?, 'active')"
		_, err := db.Exec(query, roomID, req.ItemID, req.Type)
		if err != nil {
			log.Printf("チャット部屋作成エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"room_id": roomID, "status": "active"})
	}
}

// handleRoomMessages メッセージの「送信(POST)」と「履歴取得(GET)」
func handleRoomMessages(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roomID := r.URL.Query().Get("room_id")
		if roomID == "" {
			respondWithError(w, http.StatusBadRequest, "room_id が指定されていません")
			return
		}

		switch r.Method {
		case http.MethodGet:
			// 💬 チャット履歴の取得
			query := "SELECT id, room_id, sender_id, message, created_at FROM chat_messages WHERE room_id = ? ORDER BY created_at ASC"

			rows, err := db.Query(query, roomID)
			if err != nil {
				log.Printf("メッセージ取得エラー: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			defer rows.Close()

			var messages []MessageResponse
			for rows.Next() {
				var msg MessageResponse
				var createdAtRaw []byte
				err := rows.Scan(&msg.ID, &msg.RoomID, &msg.SenderID, &msg.Message, &createdAtRaw)
				if err != nil {
					log.Printf("行スキャンエラー: %v", err)
					continue
				}
				msg.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", string(createdAtRaw))
				messages = append(messages, msg)
			}

			// 🛠️ 【修正3】for rows.Next() ループ終了後のエラーチェックを追加
			if err := rows.Err(); err != nil {
				log.Printf("データ読み込み反復エラー: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if messages == nil {
				messages = []MessageResponse{}
			}
			json.NewEncoder(w).Encode(messages)

		case http.MethodPost:
			// ✉️ メッセージの送信
			var req SendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
				return
			}

			// 🛠️ 【修正2】req.SenderID と req.Message の空チェックを追加
			if req.SenderID == "" || req.Message == "" {
				respondWithError(w, http.StatusBadRequest, "必須項目(sender_id, message)が不足しています")
				return
			}

			// 🛠️ 【修正1】保存する前に、まず先に部屋のタイプと存在チェックを実施
			var roomType string
			err := db.QueryRow("SELECT type FROM chat_rooms WHERE id = ?", roomID).Scan(&roomType)
			if err != nil {
				if err == sql.ErrNoRows {
					log.Printf("警告: 指定された room_id (%s) が存在しません。書き込みをブロックしました。", roomID)
					respondWithError(w, http.StatusNotFound, "指定されたチャット部屋が見つかりません")
					return
				}
				log.Printf("部屋タイプ取得エラー: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}

			// 部屋が存在することが確認できたので、満を持してユーザーの発言をDBに保存
			query := "INSERT INTO chat_messages (room_id, sender_id, message) VALUES (?, ?, ?)"
			_, err = db.Exec(query, roomID, req.SenderID, req.Message)
			if err != nil {
				log.Printf("メッセージ送信エラー: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}

			// 交渉部屋（negotiation）の場合のみAIが反応する
			if roomType == "negotiation" {
				// AIの発言をシミュレート
				aiReply := fmt.Sprintf("（AI交渉エージェント）「ご提示いただいた条件について検討しています。現在の提示価格から、あと5%%ほどお値引き可能であれば即決いたします！」")
				_, err = db.Exec(query, roomID, nil, aiReply)
				if err != nil {
					log.Printf("AI自動返信の保存に失敗: %v", err)
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		}
	}
}
