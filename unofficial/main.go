package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Davincible/goinsta"
)

// Configuration holds all app settings
type Configuration struct {
	Username           string            `json:"username"`
	Password           string            `json:"password"`
	ConfigPath         string            `json:"config_path"`
	CheckInterval      int               `json:"check_interval_seconds"`
	ResponseRules      map[string]string `json:"response_rules"`
	DefaultResponse    string            `json:"default_response"`
	LogFile            string            `json:"log_file"`
	RespondedUsersFile string            `json:"responded_users_file"`
}

// RespondedUsers tracks users that have received auto-replies
type RespondedUsers struct {
	Users map[int64]time.Time `json:"users"`
	mu    sync.Mutex
}

// NewRespondedUsers initializes the responded users tracker
func NewRespondedUsers(filepath string) (*RespondedUsers, error) {
	ru := &RespondedUsers{
		Users: make(map[int64]time.Time),
	}

	// Load previously responded users if file exists
	if _, err := os.Stat(filepath); err == nil {
		data, err := os.ReadFile(filepath)
		if err != nil {
			return nil, fmt.Errorf("error reading responded users file: %w", err)
		}

		var loadedUsers map[int64]time.Time
		if err := json.Unmarshal(data, &loadedUsers); err != nil {
			return nil, fmt.Errorf("error unmarshaling responded users: %w", err)
		}
		ru.Users = loadedUsers
	}

	return ru, nil
}

// HasResponded checks if a user has already received a response
func (ru *RespondedUsers) HasResponded(userID int64) bool {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	_, exists := ru.Users[userID]
	return exists
}

// MarkResponded records that a user has received a response
func (ru *RespondedUsers) MarkResponded(userID int64) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	ru.Users[userID] = time.Now()
}

// Save persists the responded users data to file
func (ru *RespondedUsers) Save(filepath string) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()

	data, err := json.MarshalIndent(ru.Users, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling responded users: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("error writing responded users file: %w", err)
	}

	return nil
}

// InstagramBot represents the auto-reply bot
type InstagramBot struct {
	insta          *goinsta.Instagram
	config         *Configuration
	respondedUsers *RespondedUsers
	logger         *log.Logger
}

// NewInstagramBot creates a new Instagram bot instance
func NewInstagramBot(config *Configuration) (*InstagramBot, error) {
	// Set up logging
	logFile, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("error opening log file: %w", err)
	}

	logger := log.New(logFile, "INSTAGRAM-BOT: ", log.LstdFlags|log.Lshortfile)

	// Initialize responded users tracker
	respondedUsers, err := NewRespondedUsers(config.RespondedUsersFile)
	if err != nil {
		logger.Printf("Error initializing responded users: %v", err)
		// Continue even if there's an error loading previous users
	}

	return &InstagramBot{
		config:         config,
		respondedUsers: respondedUsers,
		logger:         logger,
	}, nil
}

// Login authenticates with Instagram
func (bot *InstagramBot) Login() error {

	// Try to import existing session
	if _, err := os.Stat(bot.config.ConfigPath); err == nil {
		bot.logger.Println("Importing existing Instagram session")
		bot.insta, err = goinsta.Import(bot.config.ConfigPath)
		if err != nil {
			bot.logger.Printf("Failed to import session: %v. Trying to login...", err)
		} else {
			return nil
		}
	}

	// Create new session if import failed
	bot.insta = goinsta.New(bot.config.Username, bot.config.Password)
	if err := bot.insta.Login(); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Export session for future use
	if err := bot.insta.Export(bot.config.ConfigPath); err != nil {
		return fmt.Errorf("failed to export session: %w", err)
	}

	bot.logger.Println("Login successful")
	return nil
}

// Start begins the auto-reply process
func (bot *InstagramBot) Start() {
	log.Println("Starting Instagram auto-reply bot")

	ticker := time.NewTicker(time.Duration(bot.config.CheckInterval) * time.Second)
	defer ticker.Stop()

	// Initial check on startup
	bot.checkMessages()

	for range ticker.C {
		bot.checkMessages()
	}
}

// checkMessages checks for new direct messages and responds
func (bot *InstagramBot) checkMessages() {
	log.Println("Checking for new messages")

	// Get inbox
	inbox := bot.insta.Inbox
	if err := inbox.Sync(); err != nil {
		bot.logger.Printf("Error syncing inbox: %v", err)
		return
	}

	log.Printf("Found %d conversations", len(inbox.Conversations))

	// Process pending conversations
	pending := inbox.Conversations
	if err := inbox.SyncPending(); err != nil {
		log.Printf("Error syncing pending inbox: %v", err)
	} else {
		log.Println("Found pending conversations: ", pending)
		bot.processConversations(pending)
	}

	log.Println("Checking regular inbox")

	// Process regular inbox
	bot.processConversations(inbox.Conversations)

	// Save responded users
	if err := bot.respondedUsers.Save(bot.config.RespondedUsersFile); err != nil {
		bot.logger.Printf("Error saving responded users: %v", err)
	}
}

// processConversations handles multiple conversations
func (bot *InstagramBot) processConversations(conversations []*goinsta.Conversation) {
	for i := range conversations {
		conv := conversations[i]
		log.Printf("Processing conversation with %s (%d)", conv.Inviter.Username, conv.Inviter.ID)
		bot.processConversation(conv)
	}
}

// processConversation handles a single conversation
func (bot *InstagramBot) processConversation(conv *goinsta.Conversation) {

	log.Printf("Checking unread messages in conversation with %s (%d)", conv.Inviter.Username, conv.Inviter.ID)

	// Get all items in the conversation
	if err := conv.Error(); err != nil {
		log.Printf("Error syncing conversation: %v", err)
		return
	}

	// Get the first unread item
	var lastMessage *goinsta.InboxItem
	for i := len(conv.Items) - 1; i >= 0; i-- {
		item := conv.Items[i]
		if item.UserID != bot.insta.Account.ID {
			lastMessage = item
			break
		}
	}

	// if lastMessage != nil {
	// Only respond if this user hasn't received an auto-reply before
	userID := lastMessage.UserID
	log.Println("user ID: ", userID)
	log.Println("has responded to auto-reply: ", !bot.respondedUsers.HasResponded(userID))
	if bot.respondedUsers.HasResponded(userID) {
		log.Println("responding to user: ", userID)
		bot.respondToMessage(conv, lastMessage)
	}
	// }

}

// respondToMessage sends an auto-reply based on message content
func (bot *InstagramBot) respondToMessage(conv *goinsta.Conversation, item *goinsta.InboxItem) {
	// Get message text
	messageText := ""
	if item.Text != "" {
		messageText = strings.ToLower(item.Text)
	}

	// Determine appropriate response
	responseText := bot.determineResponse(messageText)

	log.Println("response: ", responseText)

	// Send the response
	if err := conv.Send(responseText); err != nil {
		bot.logger.Printf("Error sending response: %v", err)
		return
	}

	// Mark as responded
	bot.respondedUsers.MarkResponded(item.UserID)
	bot.logger.Printf("Sent auto-reply to %s: %s", conv.Users[0].Username, responseText)
}

// determineResponse selects the appropriate response based on message content
func (bot *InstagramBot) determineResponse(messageText string) string {
	// Check for keyword matches
	for pattern, response := range bot.config.ResponseRules {
		if strings.Contains(messageText, pattern) {
			return response
		}
	}

	// Return default response if no match
	return bot.config.DefaultResponse
}

// Cleanup performs cleanup operations
func (bot *InstagramBot) Cleanup() {
	// Export session for future use
	if bot.insta != nil {
		if err := bot.insta.Export(bot.config.ConfigPath); err != nil {
			bot.logger.Printf("Failed to export session during cleanup: %v", err)
		}
	}

	// Save responded users
	if err := bot.respondedUsers.Save(bot.config.RespondedUsersFile); err != nil {
		bot.logger.Printf("Error saving responded users during cleanup: %v", err)
	}

	bot.logger.Println("Bot cleanup completed")
}

func main() {
	// Load configuration
	configFile, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	var config Configuration
	if err := json.Unmarshal(configFile, &config); err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	// Create and start the bot
	bot, err := NewInstagramBot(&config)
	if err != nil {
		log.Fatalf("Error initializing bot: %v", err)
	}

	fmt.Println("cfg: ", config)

	// Set up cleanup on exit
	defer bot.Cleanup()

	// Login to Instagram
	if err := bot.Login(); err != nil {
		log.Fatalf("Error logging in: %v", err)
	}

	log.Println("Bot started")

	// Start processing messages
	bot.Start()
}
