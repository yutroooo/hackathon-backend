package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
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

			// 交渉部屋（negotiation）の場合のみ、本物のGeminiが考えて自動返信する
			if roomType == "negotiation" {
				// 💡 即時実行の無名関数で包むことで、エラー時に 'return' で安全にAI処理だけをスキップさせます
				func() {
					var itemID, itemTitle, itemDesc string
					var currentPrice int

					// 🛠️ 【修正】部屋から商品IDを特定し、きっちりエラーハンドリング
					err = db.QueryRow("SELECT item_id FROM chat_rooms WHERE id = ?", roomID).Scan(&itemID)
					if err != nil {
						log.Printf("AI用商品ID取得エラー（処理をスキップします）: %v", roomID, err)
						return // AIの自動返信処理だけを安全に終了して抜ける
					}

					// 商品詳細を取得
					err = db.QueryRow("SELECT title, current_price, description FROM items WHERE id = ?", itemID).Scan(&itemTitle, &currentPrice, &itemDesc)
					if err != nil {
						log.Printf("AI用商品情報取得エラー: %v", err)
						itemTitle = "不明な商品"
						// 商品詳細が取れなくても、最悪会話履歴だけで粘れる可能性があるので、ここは return せずに続行する優しさ
					}

					// 💡 会話の流れを汲み取るために、直近のチャット履歴を5件ほど取得する
					historyQuery := "SELECT sender_id, message FROM chat_messages WHERE room_id = ? ORDER BY created_at DESC LIMIT 5"
					rows, err := db.Query(historyQuery, roomID)

					var chatHistoryStr string
					if err == nil {
						defer rows.Close()
						for rows.Next() {
							var sID *string
							var msg string
							rows.Scan(&sID, &msg)
							senderLabel := "ユーザー"
							if sID == nil {
								senderLabel = "あなた（AI）"
							}
							chatHistoryStr = fmt.Sprintf("[%s]: %s\n%s", senderLabel, msg, chatHistoryStr)
						}
					}

					//  Gemini API の呼び出し準備
					apiKey := os.Getenv("GEMINI_API_KEY")
					ctx := r.Context()
					client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
					if err != nil {
						log.Printf("Geminiクライアント起動失敗: %v", err)
						return
					}
					defer client.Close()

					model := client.GenerativeModel("gemini-2.5-flash")

					prompt := fmt.Sprintf(`
					あなたはフリマアプリの「AI価格交渉代行エージェント」です。
					出品者の代わりに、購入希望のユーザーと丁寧かつリアルな価格交渉を行ってください。

					【対象の商品情報】
					商品名: %s
					現在の価格: %d円
					商品説明: %s

					【直近のチャット履歴】
					%s

					【現在の状況】
					ユーザーから「%s」というメッセージが届きました。

					【あなたの任務】
					- これまでの文脈と、ユーザーの最新のメッセージに合致する、自然な返答を1通だけ作成してください。
					- 口調は丁寧で、少しフリマアプリ慣れしている親しみやすい敬語（〜です、〜ます）にしてください。
					- 出品者の不利益にならないよう、いきなり大幅な値引き（2割以上など）には応じず、「間の価格」を提案するなどして、リアルに交渉を引き伸ばすか成立させてください。
					- 挨拶や余計な解説文、バッククォーツ（"""）などは一切含めず、**「ユーザーに送信するメッセージの本文」だけ**を出力してください。
					`, itemTitle, currentPrice, itemDesc, chatHistoryStr, req.Message)

					// Geminiに思考させる
					resp, err := model.GenerateContent(ctx, genai.Text(prompt))
					if err != nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
						log.Printf("Gemini応答生成失敗または空の応答: %v", err)
						return
					}

					// 生成されたテキストをAIの返答として確定
					aiReply := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

					//  AIの魂の返答を、メッセージとしてDBに刻み込む！
					_, err = db.Exec(query, roomID, nil, aiReply) // sender_id = null (AI)
					if err != nil {
						log.Printf("AI自動返信の保存に失敗: %v", err)
					}
				}() // 閉じカッコのあとに () をつけることで、定義した瞬間にこの関数を実行します
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		}
	}
}
