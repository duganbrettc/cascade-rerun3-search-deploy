package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

var (
	tokensMu sync.RWMutex
	tokens   = make(map[string]int) // token -> user id
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /api/auth/signup", handleSignup)
	mux.HandleFunc("POST /api/auth/login", handleLogin)
	mux.HandleFunc("GET /api/users/me", requireAuth(handleGetMe))
	mux.HandleFunc("PATCH /api/users/me", requireAuth(handlePatchMe))
	mux.HandleFunc("GET /api/users", handleListUsers)
	mux.HandleFunc("GET /api/users/{username}", handleGetUser)
	mux.HandleFunc("POST /api/posts", requireAuth(handleCreatePost))
	mux.HandleFunc("GET /api/posts/by/{username}", handleGetPostsByUser)
	mux.HandleFunc("POST /api/follow/{username}", requireAuth(handleFollow))
	mux.HandleFunc("DELETE /api/follow/{username}", requireAuth(handleUnfollow))
	mux.HandleFunc("GET /api/follow/status", requireAuth(handleFollowStatus))
	mux.HandleFunc("GET /api/timeline", requireAuth(handleTimeline))
	mux.HandleFunc("GET /api/search", handleSearch)

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func authFromRequest(r *http.Request) (int, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return 0, false
	}
	token := strings.TrimPrefix(h, "Bearer ")
	tokensMu.RLock()
	uid, ok := tokens[token]
	tokensMu.RUnlock()
	return uid, ok
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authFromRequest(r); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

type User struct {
	ID          int       `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName *string   `json:"display_name"`
	Bio         *string   `json:"bio,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	IsFollowing bool      `json:"is_following"`
}

func handleSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var userID int
	err = db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		req.Username, string(hash),
	).Scan(&userID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeError(w, http.StatusConflict, "username taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	token, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokensMu.Lock()
	tokens[token] = userID
	tokensMu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id":       userID,
		"session_token": token,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}

	var userID int
	var hash string
	err := db.QueryRow(
		`SELECT id, password_hash FROM users WHERE username = $1`, req.Username,
	).Scan(&userID, &hash)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokensMu.Lock()
	tokens[token] = userID
	tokensMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"session_token": token})
}

func handleGetMe(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)
	var u User
	err := db.QueryRow(
		`SELECT id, username, display_name, bio, created_at FROM users WHERE id = $1`, uid,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func handlePatchMe(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)
	var req struct {
		DisplayName *string `json:"display_name"`
		Bio         *string `json:"bio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}

	_, err := db.Exec(
		`UPDATE users SET display_name = COALESCE($1, display_name), bio = COALESCE($2, bio) WHERE id = $3`,
		req.DisplayName, req.Bio, uid,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	handleGetMe(w, r)
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	callerID, _ := authFromRequest(r)

	rows, err := db.Query(`
		SELECT u.id, u.username, u.display_name,
		       EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = u.id) AS is_following
		FROM users u
		ORDER BY u.created_at DESC
	`, callerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	type UserListItem struct {
		ID          int     `json:"user_id"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		IsFollowing bool    `json:"is_following"`
	}
	users := []UserListItem{}
	for rows.Next() {
		var u UserListItem
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsFollowing); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

func handleGetUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	callerID, _ := authFromRequest(r)

	var u User
	err := db.QueryRow(`
		SELECT u.id, u.username, u.display_name, u.bio, u.created_at,
		       EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = u.id) AS is_following
		FROM users u
		WHERE u.username = $2
	`, callerID, username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.CreatedAt, &u.IsFollowing)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type Post struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func handleCreatePost(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		writeError(w, http.StatusBadRequest, "body required")
		return
	}

	var p Post
	err := db.QueryRow(`
		INSERT INTO posts (user_id, body) VALUES ($1, $2)
		RETURNING id, user_id, body, created_at
	`, uid, req.Body).Scan(&p.ID, &p.UserID, &p.Body, &p.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// fetch username for response
	db.QueryRow(`SELECT username FROM users WHERE id = $1`, uid).Scan(&p.Username)

	writeJSON(w, http.StatusCreated, p)
}

func handleGetPostsByUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	var userID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	rows, err := db.Query(`
		SELECT p.id, p.user_id, u.username, p.body, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE u.username = $1
		ORDER BY p.created_at DESC
	`, username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	posts := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.Body, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		posts = append(posts, p)
	}
	writeJSON(w, http.StatusOK, posts)
}

func handleFollow(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)
	username := r.PathValue("username")

	var targetID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&targetID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	_, err = db.Exec(
		`INSERT INTO follows (follower_id, followee_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		uid, targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"following": true})
}

func handleUnfollow(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)
	username := r.PathValue("username")

	var targetID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&targetID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	_, err = db.Exec(
		`DELETE FROM follows WHERE follower_id = $1 AND followee_id = $2`,
		uid, targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"following": false})
}

func handleFollowStatus(w http.ResponseWriter, r *http.Request) {
	callerID, _ := authFromRequest(r)
	username := r.URL.Query().Get("username")
	if username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	var targetID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&targetID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var isFollowing bool
	db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = $2)`,
		callerID, targetID,
	).Scan(&isFollowing)

	writeJSON(w, http.StatusOK, map[string]bool{"following": isFollowing})
}

func handleTimeline(w http.ResponseWriter, r *http.Request) {
	uid, _ := authFromRequest(r)

	rows, err := db.Query(`
		SELECT p.id, p.user_id, u.username, p.body, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1
		   OR p.user_id IN (SELECT followee_id FROM follows WHERE follower_id = $1)
		ORDER BY p.created_at DESC
	`, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	posts := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.Body, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		posts = append(posts, p)
	}
	writeJSON(w, http.StatusOK, posts)
}

type SearchPostResult struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type SearchUserResult struct {
	UserID      int     `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter required")
		return
	}

	pattern := "%" + q + "%"

	postRows, err := db.Query(`
		SELECT p.id, p.user_id, u.username, p.body, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.body ILIKE $1
		ORDER BY p.created_at DESC
		LIMIT 25
	`, pattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer postRows.Close()

	posts := []SearchPostResult{}
	for postRows.Next() {
		var p SearchPostResult
		if err := postRows.Scan(&p.ID, &p.UserID, &p.Username, &p.Body, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		posts = append(posts, p)
	}

	userRows, err := db.Query(`
		SELECT id, username, display_name
		FROM users
		WHERE username ILIKE $1 OR display_name ILIKE $1
		ORDER BY username
		LIMIT 25
	`, pattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer userRows.Close()

	users := []SearchUserResult{}
	for userRows.Next() {
		var u SearchUserResult
		if err := userRows.Scan(&u.UserID, &u.Username, &u.DisplayName); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		users = append(users, u)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"posts": posts,
		"users": users,
	})
}
