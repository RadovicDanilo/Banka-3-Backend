package repo

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
	"github.com/RAF-SI-2025/Banka-3-Backend/services/user/internal/model"
)

// GetUserByEmail retrieves a user (employee or client) by email.
func (r *Repository) GetUserByEmail(email string) (*User, error) {
	query := `
		SELECT email, password, salt_password FROM employees WHERE email = $1
		UNION ALL
		SELECT email, password, salt_password FROM clients WHERE email = $1
		LIMIT 1
	`

	var user User
	err := r.database.QueryRow(query, email).Scan(&user.Email, &user.HashedPassword, &user.Salt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserIdByEmail returns the user ID (from employees or clients) for a given email.
func (r *Repository) GetUserIdByEmail(email string) (*uint64, error) {
	query := `
		SELECT id FROM employees WHERE email = $1
		UNION ALL
		SELECT id FROM clients WHERE email = $1
		LIMIT 1
	`
	var id uint64
	err := r.database.QueryRow(query, email).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// GetPermissionsByNames retrieves all permission models matching the provided names.
// It logs a warning if some permissions requested are not found in the database.
func (r *Repository) GetPermissionsByNames(ctx context.Context, names []string) ([]model.Permission, error) {
	var permissions []model.Permission

	// Fetch all permissions where the name is in the provided slice
	err := r.gorm_db.WithContext(ctx).Where("name IN ?", names).Find(&permissions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch permissions: %w", err)
	}

	// Optional: Log if the count doesn't match to replicate your "skipping" warning logic
	if len(permissions) != len(names) {
		logger.FromContext(ctx).WarnContext(ctx, "some permissions were not found in the database",
			"requested", len(names),
			"found", len(permissions))
	}

	return permissions, nil
}

// LogoutUser handles the database transaction for revoking tokens during logout.
func (r *Repository) LogoutUser(email string) error {
	tx, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = r.RevokeRefreshTokensByEmail(tx, email)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}
