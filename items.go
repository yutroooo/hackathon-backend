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
		//
		defer func() {
			if p := recover(); p != nil { //変数名を r から p に修正して安全に
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

		// 1. 環境変数から Gemini の API キーを取得
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			log.Printf("エラー: GEMINI_API_KEY が設定されていません")
			respondWithError(w, http.StatusInternalServerError, "AI設定エラー: キーが空です")
			return
		}

		// 2. Gemini クライアントの初期化
		ctx := r.Context()
		client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			log.Printf("Geminiクライアント初期化失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "AI接続失敗: "+err.Error())
			return
		}
		defer client.Close()

		// 3. 安定版の「models/gemini-1.5-flash」を指定
		model := client.GenerativeModel("gemini-2.5-flash")
		model.ResponseMIMEType = "application/json"

		// 4. プロンプトの作成
		imageUrl := "なし"
		if len(req.ImageURLs) > 0 {
			imageUrl = req.ImageURLs[0]
		}

		prompt := fmt.Sprintf(`
       あなたはフリマアプリの凄腕出品シニアアドバイザーです。
       ユーザーから提出された以下の情報（画像URL等）を分析し、魅力的な出品情報を生成してください。

       【入力された画像URL】: %s

       【出力フォーマット】
       必ず以下のJSONフォーマットの形式だけで返答してください。余計な解説や前置き、バッククォーツ（json ）などは一切含めないでください。

       {
          "title": "ここに生成した商品タイトル（50文字以内）",
          "description": "ここに生成した魅力的な商品説明文。状態やハッシュタグなども含む",
          "category": "ここに適切なカテゴリ名",
          "recommended_price": 適切な推奨価格を数値（整数）で
       }
       `, imageUrl)

		// 5. Gemini に聞いてみる
		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Gemini呼び出し失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "GeminiのAPI叩く所で失敗: "+err.Error())
			return
		}

		// 6. 安全ガード
		if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			respondWithError(w, http.StatusInternalServerError, "AIからの応答が空っぽ、または構造が異常です")
			return
		}

		// AIの返答テキスト（JSON文字列）を取り出す
		aiJsonStr := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(aiJsonStr))
	}
}
