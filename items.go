package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// handleCreateItem 商品出品 (POST /api/items)
func handleCreateItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req CreateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		if req.SellerID == "" || req.Title == "" || req.InitialPrice <= 0 {
			respondWithError(w, http.StatusBadRequest, "必須項目が不足しています")
			return
		}

		// ユニークな商品IDの生成
		itemID := fmt.Sprintf("ITM%d", time.Now().UnixNano())

		// 【修正】クエリのプレースホルダー数に合わせて、最後に req.ImageURL をしっかりと追加
		query := `INSERT INTO items (id, seller_id, title, description, initial_price, current_price, category, status, image_url) 
               VALUES (?, ?, ?, ?, ?, ?, ?, 'on_sale', ?)`

		_, err := db.Exec(query, itemID, req.SellerID, req.Title, req.Description, req.InitialPrice, req.InitialPrice, req.Category, req.ImageURL)
		if err != nil {
			log.Printf("商品出品エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": itemID, "status": "on_sale"})
	}
}

// handleGetItems 商品一覧取得 (GET /api/items)
func handleGetItems(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		// 変数の重複宣言を解消し、Scanの構造体と完全に一致するSELECT文に変更
		query := `
          SELECT id, title, description, current_price, seller_id, image_url 
          FROM items 
          WHERE status = 'on_sale' 
          ORDER BY created_at DESC`

		rows, err := db.Query(query)
		if err != nil {
			log.Printf("商品一覧取得エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		defer rows.Close()

		var items []Item

		for rows.Next() {
			var item Item
			var imageURL sql.NullString

			// Scanの数とSELECTのカラム順を完全にシンクロ
			if err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.CurrentPrice, &item.SellerID, &imageURL); err != nil {
				log.Printf("商品スキャンエラー: %v", err)
				continue
			}

			if imageURL.Valid {
				item.ImageURL = imageURL.String
			}

			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		if items == nil {
			items = []Item{} // 型不一致を防ぐために宣言通りの空スライスを返却
		}
		json.NewEncoder(w).Encode(items)
	}
}

// handleAISuggest AI出品サポート (POST /api/items/ai-suggest)
func handleAISuggest(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf(" AIハンドラー内でパニック発生: %v", p)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("AI内部即死パニック: %v", p)})
			}
		}()

		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req AISuggestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		if len(req.ImageURLs) == 0 || req.ImageURLs[0] == "" {
			respondWithError(w, http.StatusBadRequest, "画像URLがありません")
			return
		}

		imgURL := req.ImageURLs[0]

		// ========================================================
		// 🚀 GoがCloudinaryから直接「画像データ」をダウンロード！
		// ========================================================
		respImg, err := http.Get(imgURL)
		if err != nil {
			log.Printf("画像ダウンロード失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "画像の取得に失敗しました")
			return
		}
		defer respImg.Body.Close()

		imgBytes, err := io.ReadAll(respImg.Body)
		if err != nil {
			log.Printf("画像読み取り失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "画像の読み取りに失敗しました")
			return
		}

		// ========================================================
		// 🤖 Geminiの目に画像を直接押し付けて鑑定させる！
		// ========================================================
		apiKey := os.Getenv("GEMINI_API_KEY")
		ctx := r.Context()
		client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			log.Printf("Geminiクライアント起動失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "AIの起動に失敗しました")
			return
		}
		defer client.Close()

		model := client.GenerativeModel("gemini-2.5-flash")
		model.ResponseMIMEType = "application/json"

		imagePart := genai.ImageData("jpeg", imgBytes)
		promptPart := genai.Text(`あなたはこの画像を鑑定するプロの査定員です。
       この画像に写っている商品の「商品名(title)」「推奨価格(recommended_price)」「魅力的な商品説明(description)」を以下のJSONフォーマットで出力してください。
       {
           "title": "商品名",
           "recommended_price": 3000,
           "description": "商品の詳細な説明"
       }`)

		resp, err := model.GenerateContent(ctx, imagePart, promptPart)
		if err != nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			log.Printf("Gemini応答生成失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "AIの鑑定に失敗しました")
			return
		}

		// ========================================================
		// AIの出した答え（JSON）をそのままフロントに返す！
		// ========================================================
		aiJsonStr := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(aiJsonStr))
	}
} // 🎯 【修正】古い残骸コードをすべて消去し、ここで綺麗に関数を閉じました！
