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

// handleCreateRoom チャット部屋の作成・または既存の部屋を返す (POST /api/rooms)
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

		// ========================================================
		// 🎯 【超重要ハック】すでにこの商品に対するアクティブな部屋があるかチェック！
		// ========================================================
		var existingRoomID string
		checkQuery := "SELECT id FROM chat_rooms WHERE item_id = ? AND type = ? AND status = 'active' LIMIT 1"
		err := db.QueryRow(checkQuery, req.ItemID, req.Type).Scan(&existingRoomID)

		if err == nil {
			// 🎉 すでに部屋が存在していた！新しく作らずに、その部屋IDを返して合流させる！
			log.Printf("👥 既存のチャット部屋を発見。合流させます。RoomID: %s, ItemID: %s", existingRoomID, req.ItemID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK) // 既存返却なので 200 OK
			json.NewEncoder(w).Encode(map[string]string{"room_id": existingRoomID, "status": "active"})
			return
		} else if err != sql.ErrNoRows {
			// エラーが「行が見つからない」以外（DB接続不良など）なら500で落とす
			log.Printf("既存チャット部屋確認エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		// ========================================================
		// 🟢 部屋がまだない場合だけ、以下で新規に作成する（元のロジック）
		// ========================================================

		// ユニークな部屋IDの生成 (RM+ナノ秒)
		roomID := fmt.Sprintf("RM%d", time.Now().UnixNano())

		// DBに部屋を挿入
		query := "INSERT INTO chat_rooms (id, item_id, type, status) VALUES (?, ?, ?, 'active')"
		_, err = db.Exec(query, roomID, req.ItemID, req.Type)
		if err != nil {
			log.Printf("チャット部屋作成エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated) // 新規作成なので 21 Created
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
			// チャット履歴の取得
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

			//  【修正3】for rows.Next() ループ終了後のエラーチェックを追加
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
			// メッセージの送信
			var req SendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
				return
			}

			// 【修正2】req.SenderID と req.Message の空チェックを追加
			if req.SenderID == "" || req.Message == "" {
				respondWithError(w, http.StatusBadRequest, "必須項目(sender_id, message)が不足しています")
				return
			}

			// 保存する前に、まず先に部屋のタイプと存在チェックを実施
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

			if roomType == "negotiation" {
				// 即時実行の無名関数で包むことで、エラー時に 'return' で安全にAI処理だけをスキップさせます
				func() {
					// 無名関数の中で独立した err を宣言し、外側との干渉によるコンパイルエラーを完全に防ぐ！
					var err error
					var itemID, itemTitle, itemDesc, sellerID string // 末尾に sellerID を追加
					var currentPrice int

					// 部屋から商品IDを特定
					err = db.QueryRow("SELECT item_id FROM chat_rooms WHERE id = ?", roomID).Scan(&itemID)
					if err != nil {
						log.Printf("AI用商品ID取得エラー（処理をスキップします）: %v", err)
						return
					}

					// 商品詳細を取得
					err = db.QueryRow("SELECT title, current_price, description FROM items WHERE id = ?", itemID).Scan(&itemTitle, &currentPrice, &itemDesc, &sellerID)
					if err != nil {
						log.Printf("AI用商品情報取得エラー: %v", err)
						itemTitle = "不明な商品"
					}

					// 会話の流れを汲み取るために、直近のチャット履歴を5件ほど取得する
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

					// 今届いたメッセージのテキスト(req.Message)をDBから直接逆引きして、送信者を出品者本人か確定させる！
					var actualSenderID *string
					checkQuery := "SELECT sender_id FROM chat_messages WHERE room_id = ? AND message = ? ORDER BY created_at DESC LIMIT 1"
					errCheck := db.QueryRow(checkQuery, roomID, req.Message).Scan(&actualSenderID)

					if errCheck == nil && actualSenderID != nil && *actualSenderID == sellerID {
						log.Printf(" 精密検証成功：このメッセージの送信者は出品者本人です。AI自動応答を100%スキップします。")
						return
					}

					// Gemini API の呼び出し準備 (ここから下はそのまま)
					// ーーーこの下に Gemini API の呼び出し準備（client, err := genai.NewClient...）が続きますーーー

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

					// Geminiに「構造化したJSONで返せ」と命令するモードを設定
					model.ResponseMIMEType = "application/json"

					//  プロンプトをJSON返却仕様＆合意判定仕様にチューニング
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
      - 口調は丁寧で、親しみやすい敬語（〜です、〜ます）にしてください。
      - いきなり大幅な値引きには応じず、「間の価格」を提案するなどして、リアルに交渉を引き伸ばすか成立させてください。
      - **最重要**: ユーザーの提案した価格、あるいはお互いの妥協点で価格交渉が「成立・合意」に達したと判断した場合は、"is_agreed" を true にし、"agreed_price" に合意した金額を整数（数値）で設定してください。まだ交渉が続いている場合は "is_agreed" は false にしてください。

      【出力フォーマット】
      必ず以下のJSONフォーマットの形式だけで返答してください。余計な解説や前置き、バッククォーツなどは一切含めないでください。

      {
         "message": "ユーザーに送信するチャットメッセージ本文（解説などは含めない単一のテキスト）",
         "is_agreed": 価格交渉が成立・合意に達したかどうかの真偽値 (true / false),
         "agreed_price": 交渉が成立した場合の合意価格（数値）。未成立なら0
      }
      `, itemTitle, currentPrice, itemDesc, chatHistoryStr, req.Message)

					// Geminiに思考させる
					resp, err := model.GenerateContent(ctx, genai.Text(prompt))
					if err != nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
						log.Printf("Gemini応答生成失敗または空の応答: %v", err)
						return
					}

					// AIの返答（JSON文字列）を取り出す
					aiJsonStr := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

					var aiRes struct {
						Message     string `json:"message"`
						IsAgreed    bool   `json:"is_agreed"`
						AgreedPrice int    `json:"agreed_price"`
					}

					if err := json.Unmarshal([]byte(aiJsonStr), &aiRes); err != nil {
						log.Printf("AI応答のJSONパース失敗、通常のテキストとしてフォールバックします: %v", err)
						aiRes.Message = aiJsonStr
					}

					//  AIの確定したメッセージを、DBに
					// (query は外側のスコープに定義されているものをそのままキャプチャします)
					_, err = db.Exec(query, roomID, nil, aiRes.Message)
					if err != nil {
						log.Printf("AI自動返信の保存に失敗: %v", err)
					}

					// 交渉成立（is_agreed == true）なら、商品の現在価格を上書き更新する！
					if aiRes.IsAgreed && aiRes.AgreedPrice > 0 {
						log.Printf("🎉 【価格交渉成立】価格を %d 円に更新します。商品ID: %s", aiRes.AgreedPrice, itemID)
						updatePriceQuery := "UPDATE items SET current_price = ? WHERE id = ?"
						_, err = db.Exec(updatePriceQuery, aiRes.AgreedPrice, itemID)
						if err != nil {
							log.Printf("DBの商品価格更新エラー: %v", err)
						}
					}
				}()
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		}

	}
}
