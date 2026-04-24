package utils

import (
	"context"
	"crimpy/backend/internal/db"
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
)

const (
	AdminEmail     = "admin"
	AdminFirstname = "admin"
	AdminLastname  = "admin"
	DevPassword    = "admin"
)

// GenerateRandomPassword generates a random password
func GenerateRandomPassword(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// InitializeAdminAccount creates or verifies the admin account
func InitializeAdminAccount(queries *db.Queries) error {
	ctx := context.Background()

	// Check if admin already exists
	existingAdmin, err := queries.GetUserByEmail(ctx, AdminEmail)
	if err == nil {
		// Admin exists
		log.Println("Admin account already exists")
		log.Printf("Admin Email: %s", existingAdmin.Email)
		return nil
	}

	if err != pgx.ErrNoRows {
		// Some other error occurred
		return err
	}

	// Admin doesn't exist, create it
	var password string
	env := os.Getenv("ENV")

	if env == "production" || env == "preproduction" {
		// Generate random password for (pre-)production
		randomPass, err := GenerateRandomPassword(16)
		if err != nil {
			return err
		}
		password = randomPass
	} else {
		// Use fixed password for development
		password = DevPassword
	}

	// Hash the password
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return err
	}

	// Create admin user
	admin, err := queries.CreateAdminUser(ctx, db.CreateAdminUserParams{
		Email:     AdminEmail,
		Firstname: AdminFirstname,
		Lastname:  AdminLastname,
		Password:  hashedPassword,
	})

	if err != nil {
		return err
	}

	// Log the credentials
	log.Println("=====================================")
	log.Println("SUPER ADMIN ACCOUNT CREATED")
	log.Println("=====================================")
	log.Printf("Email:    %s", admin.Email)
	log.Printf("Password: %s", password)
	log.Println("=====================================")
	log.Println("IMPORTANT: Save these credentials!")
	log.Println("=====================================")

	return nil
}
