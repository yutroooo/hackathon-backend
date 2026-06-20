package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
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

		// ユニークな商品IDの生成 (VARCHAR(26) に合わせて ITM+ナノ秒)
		itemID := fmt.Sprintf("ITM%d", time.Now().UnixNano())

		// 初期価格(initial_price)と現在価格(current_price)は出品時は同じ
		query := `INSERT INTO items (id, seller_id, title, description, initial_price, current_price, category, status) 
				  VALUES (?, ?, ?, ?, ?, ?, ?, 'on_sale')`

		_, err := db.Exec(query, itemID, req.SellerID, req.Title, req.Description, req.InitialPrice, req.InitialPrice, req.Category)
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

		// 出品中の商品を新しい順に取得
		query := "SELECT id, seller_id, buyer_id, title, description, initial_price, current_price, category, status, created_at FROM items WHERE status = 'on_sale' ORDER BY created_at DESC"
		rows, err := db.Query(query)
		if err != nil {
			log.Printf("商品一覧取得エラー: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		defer rows.Close()

		var items []ItemResponse
		for rows.Next() {
			var item ItemResponse
			var createdAtRaw []byte // TIMESTAMP型を安全に読み込むため一時的にバイト配列で受ける

			err := rows.Scan(&item.ID, &item.SellerID, &item.BuyerID, &item.Title, &item.Description, &item.InitialPrice, &item.CurrentPrice, &item.Category, &item.Status, &createdAtRaw)
			if err != nil {
				log.Printf("行スキャンエラー: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}

			// 文字列としてパースされた時間を time.Time に変換（簡易処理）
			item.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", string(createdAtRaw))
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		if items == nil {
			items = []ItemResponse{} // null ではなく空配列 [] を返す
		}
		json.NewEncoder(w).Encode(items)
	}
}

// handleAISuggest AI出品サポート (POST /api/items/ai-suggest)
func handleAISuggest(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}

		var req AISuggestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}

		//  ハッカソンモック用の暫定AIレスポンス
		// 本番はここに Gemini API や OpenAI API のコールを組み込む
		log.Printf("AI提案要求を受け取りました。画像URL数: %d", len(req.ImageURLs))

		suggested := AISuggestResponse{
			Title:            "【極美品】限定レアスニーカー 27cm",
			Description:      "ハッカソン会場で一度だけ着用した限定モデルです。状態は非常に良く、ソールの減りもほぼありません。即購入OKです！",
			Category:         "ファッション・靴",
			RecommendedPrice: 12800,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(suggested)
	}
}
