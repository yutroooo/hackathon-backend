package main

import "time"

// ==========================================
// 1. 認証・ユーザー系
// ==========================================

// RegisterRequest 新規会員登録のリクエスト
type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest ログインのリクエスト
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse 認証成功時にフロントに返す共通データ
type AuthResponse struct {
	Token  string `json:"token"` // ユーザーIDを使用
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

// ==========================================
// 2. 商品（出品・一覧）系
// ==========================================

// AISuggestRequest AI出品サポートへのリクエスト
type AISuggestRequest struct {
	ImageURLs []string `json:"image_urls"` // フロントから送られてくる写真のURL
}

// AISuggestResponse AIが提案してくれる出品情報
type AISuggestResponse struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	Category         string `json:"category"`
	RecommendedPrice int    `json:"recommended_price"`
}

// CreateItemRequest ユーザーが確定させた出品リクエスト
type CreateItemRequest struct {
	SellerID     string `json:"seller_id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	InitialPrice int    `json:"initial_price"`
	Category     string `json:"category"`
}

// ItemResponse フロントに返す商品情報（一覧・詳細共通）
type ItemResponse struct {
	ID           string    `json:"id"`
	SellerID     string    `json:"seller_id"`
	BuyerID      *string   `json:"buyer_id,omitempty"` // まだ売れてなければnullになるのでポインタ
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	InitialPrice int       `json:"initial_price"`
	CurrentPrice int       `json:"current_price"`
	Category     string    `json:"category"`
	Status       string    `json:"status"` // 'on_sale', 'reserved', 'sold', 'deleted'
	CreatedAt    time.Time `json:"created_at"`
}

// ==========================================
// 3. チャット・交渉・取引系
// ==========================================

// CreateRoomRequest 交渉部屋（またはDM部屋）を立ち上げるリクエスト
type CreateRoomRequest struct {
	ItemID string `json:"item_id"`
	Type   string `json:"type"` // 'negotiation' または 'transaction'
}

// RoomResponse 作成された（または取得した）チャット部屋情報
type RoomResponse struct {
	ID        string    `json:"id"`
	ItemID    string    `json:"item_id"`
	Type      string    `json:"type"`   // 'negotiation', 'transaction'
	Status    string    `json:"status"` // 'active', 'completed', 'failed'
	CreatedAt time.Time `json:"created_at"`
}

// SendMessageRequest メッセージ送信時のリクエスト
type SendMessageRequest struct {
	SenderID string `json:"sender_id"`
	Message  string `json:"message"`
}

// MessageResponse フロントに返すメッセージ単体のデータ
type MessageResponse struct {
	ID        int64     `json:"id"`
	RoomID    string    `json:"room_id"`
	SenderID  *string   `json:"sender_id,omitempty"` // AI発言やシステム通知の場合はnull
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// CommitRequest 取引を確定させる（購入完了）際のリクエスト
type CommitRequest struct {
	BuyerID string `json:"buyer_id"`
	Price   int    `json:"price"` // 最終交渉妥結価格
}

// ==========================================
// 4. エラー共通系
// ==========================================

// ErrorResponse フロントエンドにエラー理由を綺麗に伝えるための共通型
type ErrorResponse struct {
	Error string `json:"error"`
}
