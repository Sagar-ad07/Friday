package friday

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// User is someone who signed up for the Friday app.
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Plan    string `json:"plan,omitempty"`
	Created string `json:"created"`
}

// signupRequest is the validated input for POST /api/signup.
type signupRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password"`
	Plan     string `json:"plan"`
}

var (
	usersOnce sync.Once
	usersMu   sync.RWMutex
	users     []User
	usersFile = filepath.Join(ProjectRoot, "data", "users.json")
)

// GetUsers returns a snapshot of all users (loading from disk on first call).
func GetUsers() []User {
	usersOnce.Do(func() {
		if data, err := os.ReadFile(usersFile); err == nil {
			if json.Unmarshal(data, &users) != nil {
				users = nil
			}
		}
		log.Printf("[USERS] store ready: %s (%d users)", usersFile, len(users))
	})
	usersMu.RLock()
	out := make([]User, len(users))
	copy(out, users)
	usersMu.RUnlock()
	return out
}

func saveUsers() {
	usersMu.RLock()
	data, _ := json.MarshalIndent(users, "", "  ")
	usersMu.RUnlock()
	os.MkdirAll(filepath.Dir(usersFile), 0755)
	_ = os.WriteFile(usersFile, data, 0644)
}

// SignupPageHandler serves the signup page.
func (s *Server) SignupPageHandler(c *gin.Context) {
	c.File(filepath.Join(ProjectRoot, "webui", "signup.html"))
}

// SignupHandler creates a new user.
func (s *Server) SignupHandler(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "name and a valid email are required"})
		return
	}

	GetUsers()
	usersMu.Lock()
	defer usersMu.Unlock()
	for _, u := range users {
		if u.Email == req.Email {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "email already registered"})
			return
		}
	}
	u := User{
		ID:      fmt.Sprintf("u_%d", time.Now().UnixNano()),
		Name:    req.Name,
		Email:   req.Email,
		Plan:    req.Plan,
		Created: time.Now().Format(time.RFC3339),
	}
	users = append(users, u)
	usersMu.Unlock()
	saveUsers()

	log.Printf("[USERS] new user: %s <%s> plan=%s", u.Name, u.Email, u.Plan)
	c.JSON(http.StatusOK, gin.H{"success": true, "user": u, "total": len(users)})
}

// UsersListHandler returns all users.
func (s *Server) UsersListHandler(c *gin.Context) {
	us := GetUsers()
	c.JSON(http.StatusOK, gin.H{"users": us, "total": len(us)})
}
