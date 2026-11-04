package service

import (
	"context"
	"database/sql"
	"os/exec"
	"os/user"
	"runtime"
	"strings"

	"instaboard/internal/models"
	"instaboard/internal/repository"
)

// AuthService handles the local app's single user.
type AuthService struct {
	userRepo *repository.UserRepo
}

// NewAuthService creates a new AuthService.
func NewAuthService(userRepo *repository.UserRepo) *AuthService {
	return &AuthService{userRepo: userRepo}
}

// LocalUser returns the app's only user (the one flagged is_local), creating and flagging it on first run,
// and keeps its name in sync with the computer's account name. Without a flag, an existing database's
// earliest user is adopted so earlier saves stay theirs.
func (s *AuthService) LocalUser(ctx context.Context, name string) (*models.User, error) {
	u, err := s.userRepo.LocalUser(ctx)
	if err != nil {
		return nil, err
	}
	if u == nil {
		if u, err = s.userRepo.First(ctx); err != nil {
			return nil, err
		}
		if u == nil {
			u = &models.User{Name: strPtr(name)}
			if err := s.userRepo.Create(ctx, u); err != nil {
				return nil, err
			}
		}
		if err := s.userRepo.SetLocal(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	if name != "" && u.Name.String != name {
		u.Name = strPtr(name)
		if err := s.userRepo.Update(ctx, u); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// AccountName returns the full name of the computer account running Instaboard (e.g. "Chandra Shekar"),
// falling back to the login name.
func AccountName() string {
	if runtime.GOOS == "darwin" {
		// Works without cgo, unlike os/user's full-name lookup on macOS.
		if out, err := exec.Command("id", "-F").Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				return name
			}
		}
	}
	if u, err := user.Current(); err == nil {
		// On Linux the name field may carry extra comma-separated details.
		if name := strings.TrimSpace(strings.Split(u.Name, ",")[0]); name != "" {
			return name
		}
		if u.Username != "" {
			return u.Username
		}
	}
	return "You"
}

func strPtr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
