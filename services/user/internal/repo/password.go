package repo

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/RAF-SI-2025/Banka-3-Backend/services/user/internal/utils"
)

// UpsertPasswordActionToken creates or updates a password action token.
func (r *Repository) UpsertPasswordActionToken(email, actionType string, hashedToken []byte, validUntil time.Time) error {
	query := `
	INSERT INTO password_action_tokens (email, action_type, hashed_token, valid_until, used)
	VALUES ($1, $2, $3, $4, FALSE)
	ON CONFLICT (email, action_type)
	DO UPDATE SET
		hashed_token = excluded.hashed_token,
		valid_until = excluded.valid_until,
		used = FALSE,
		used_at = NULL
	`
	_, err := r.database.Exec(query, email, actionType, hashedToken, validUntil)
	if err != nil {
		return fmt.Errorf("upserting password action token: %w", err)
	}
	return nil
}

// ConsumePasswordActionToken marks a token as used and returns the associated email and action type.
func (r *Repository) ConsumePasswordActionToken(tx *sql.Tx, hashedToken []byte) (string, string, error) {
	var email string
	var actionType string
	err := tx.QueryRow(`
		SELECT email, action_type
		FROM password_action_tokens
		WHERE hashed_token = $1 AND used = FALSE AND valid_until > NOW()
		FOR UPDATE
	`, hashedToken).Scan(&email, &actionType)
	if err == sql.ErrNoRows {
		return "", "", ErrInvalidPasswordActionToken
	}
	if err != nil {
		return "", "", fmt.Errorf("querying password action token: %w", err)
	}

	_, err = tx.Exec(`
		UPDATE password_action_tokens
		SET used = TRUE, used_at = NOW()
		WHERE email = $1 AND action_type = $2
	`, email, actionType)
	if err != nil {
		return "", "", fmt.Errorf("marking password action token used: %w", err)
	}
	return email, actionType, nil
}

// UpdatePasswordByEmail updates password for either employee or client.
func (r *Repository) UpdatePasswordByEmail(tx *sql.Tx, email string, hashedPassword []byte) error {
	employeeRes, err := tx.Exec(`
		UPDATE employees
		SET password = $1, updated_at = NOW()
		WHERE email = $2
	`, hashedPassword, email)
	if err != nil {
		return fmt.Errorf("updating employee password: %w", err)
	}
	employeeRows, err := employeeRes.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading employee affected rows: %w", err)
	}
	if employeeRows > 0 {
		return nil
	}

	clientRes, err := tx.Exec(`
		UPDATE clients
		SET password = $1, updated_at = NOW()
		WHERE email = $2
	`, hashedPassword, email)
	if err != nil {
		return fmt.Errorf("updating client password: %w", err)
	}
	clientRows, err := clientRes.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading client affected rows: %w", err)
	}
	if clientRows == 0 {
		return fmt.Errorf("user not found for email")
	}
	return nil
}

// ActivateEmployeeByEmail marks an employee as active.
func (r *Repository) ActivateEmployeeByEmail(tx *sql.Tx, email string) error {
	if _, err := tx.Exec(`UPDATE employees SET active = true, updated_at = NOW() WHERE email = $1`, email); err != nil {
		return fmt.Errorf("activating employee: %w", err)
	}
	return nil
}

// ResetPasswordWithToken orchestrates the token consumption, password update, and account activation.
func (r *Repository) ResetPasswordWithToken(token, newPassword string) (string, error) {
	tx, err := r.database.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// If your Consume func returns (string, string, error), just use '_' for the second value
	email, _, err := r.ConsumePasswordActionToken(tx, utils.HashValue(token))
	if err != nil {
		return "", err
	}

	user, err := r.GetUserByEmail(email)
	if err != nil || user == nil {
		return "", fmt.Errorf("user lookup failed")
	}

	hashedPassword := utils.HashPassword(newPassword, user.Salt)

	if err := r.UpdatePasswordByEmail(tx, email, hashedPassword); err != nil {
		return "", err
	}

	// Activate the employee and revoke old sessions as part of the password change
	if err := r.ActivateEmployeeByEmail(tx, email); err != nil {
		return "", err
	}

	if err := r.RevokeRefreshTokensByEmail(tx, email); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return email, nil
}
