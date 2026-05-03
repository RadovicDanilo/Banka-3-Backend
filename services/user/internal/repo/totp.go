package repo

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/RAF-SI-2025/Banka-3-Backend/services/user/internal/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SetTempTOTPSecret stores a temporary TOTP secret for a user.
func (r *Repository) SetTempTOTPSecret(tx *sql.Tx, id uint64, secret string) error {
	_, err := tx.Exec(`
		INSERT INTO verification_codes (client_id, temp_secret, temp_created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (client_id)
		DO UPDATE SET
			temp_secret = EXCLUDED.temp_secret,
			temp_created_at = NOW()
	`, id, secret)
	return err
}

// GetTempSecret retrieves the temporary TOTP secret for a user.
func (r *Repository) GetTempSecret(tx *sql.Tx, id uint64) (*string, error) {
	var tempSecret string
	row := tx.QueryRow(`
		SELECT temp_secret
		FROM verification_codes
		WHERE client_id = $1
		FOR UPDATE
	`, id)
	err := row.Scan(&tempSecret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &tempSecret, nil
}

// GetSecret returns the active TOTP secret for a user.
func (r *Repository) GetSecret(id uint64) (*string, error) {
	var secret string
	row := r.database.QueryRow(`
		SELECT secret
		FROM verification_codes
		WHERE client_id = $1 AND enabled = TRUE
	`, id)
	err := row.Scan(&secret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &secret, nil
}

// EnableTOTP activates TOTP for a user and stores the permanent secret.
func (r *Repository) EnableTOTP(tx *sql.Tx, id uint64, tempSecret string) error {
	_, err := tx.Exec(`
		UPDATE verification_codes
		SET enabled = TRUE,
		    secret = $1,
		    temp_secret = NULL
		WHERE client_id = $2
	`, tempSecret, id)
	return err
}

// DisableTOTP deactivates TOTP for a user.
func (r *Repository) DisableTOTP(tx *sql.Tx, id uint64) error {
	_, err := tx.Exec(`
		UPDATE verification_codes
		SET enabled = FALSE
		WHERE client_id = $1
	`, id)
	return err
}

// DeleteOldCodes removes all backup codes for a user.
func (r *Repository) DeleteOldCodes(tx *sql.Tx, id uint64) error {
	_, err := tx.Exec(`
		DELETE FROM backup_codes
		WHERE client_id = $1
	`, id)
	return err
}

// InsertGeneratedCodes inserts new backup codes for a user.
func (r *Repository) InsertGeneratedCodes(tx *sql.Tx, id uint64, codes []string) error {
	if err := r.DeleteOldCodes(tx, id); err != nil {
		return err
	}
	query := "INSERT INTO backup_codes (client_id, token) VALUES"
	values := []any{}
	paramIdx := 1
	for _, code := range codes {
		query += fmt.Sprintf("($%d, $%d),", paramIdx, paramIdx+1)
		paramIdx += 2
		values = append(values, id, code)
	}
	query = query[:len(query)-1]
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(values...)
	return err
}

// Status returns whether TOTP is currently enabled for a user.
func (r *Repository) Status(id uint64) (*bool, error) {
	tx, err := r.database.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var active bool
	row := tx.QueryRow(`
		SELECT enabled
		FROM verification_codes
		WHERE client_id = $1
		FOR UPDATE
	`, id)
	err = row.Scan(&active)
	if err != nil {
		dummy := false
		if errors.Is(err, sql.ErrNoRows) {
			return &dummy, nil
		}
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return &active, nil
}

// TryBurnBackupCode attempts to use a backup code; returns true if successful.
func (r *Repository) TryBurnBackupCode(id uint64, code string) (*bool, error) {
	query := `
		UPDATE backup_codes
		SET used = TRUE
		WHERE client_id = $1 AND token = $2
	`
	res, err := r.database.Exec(query, id, code)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	success := rows == 1
	return &success, nil
}

func (r *Repository) DisableConfirm(userId uint64, token string) error {
	tx, err := r.database.Begin()
	if err != nil {
		return status.Error(codes.Internal, "starting transaction failed")
	}
	defer func() { _ = tx.Rollback() }()

	_, _, err = r.ConsumePasswordActionToken(tx, utils.HashValue(token))
	if err != nil {
		if errors.Is(err, ErrInvalidPasswordActionToken) {
			return status.Error(codes.InvalidArgument, "invalid or expired token")
		}
		return status.Error(codes.Internal, "token validation failed")
	}

	err = r.DeleteOldCodes(tx, userId)
	if err != nil {
		return err
	}

	err = r.DisableTOTP(tx, userId)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return status.Error(codes.Internal, "committing transaction failed")
	}

	return nil
}

func (r *Repository) EnrollBegin(userId uint64, secret string) error {
	tx, err := r.database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	active, err := r.Status(userId)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return status.Error(codes.NotFound, "user not found")
		}
		return err
	}
	if *active {
		return status.Error(20, "totp already enabled")
	}

	err = r.SetTempTOTPSecret(tx, userId, secret)
	if err != nil {
		return err
	}
	err = tx.Commit()

	if err != nil {
		return err
	}
	return nil
}

// FinalizeTOTPEnrollment enables TOTP and saves backup codes in a single transaction.
func (r *Repository) FinalizeTOTPEnrollment(userId uint64, secret string, backupCodes []string) error {
	tx, err := r.database.Begin() // using r.db instead of s.db
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Enable TOTP
	err = r.EnableTOTP(tx, userId, secret)
	if err != nil {
		return err
	}

	// Insert backup codes
	err = r.InsertGeneratedCodes(tx, userId, backupCodes)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetTempSecretNoTx helper to fetch temp secret without requiring a caller-managed transaction.
func (r *Repository) GetTempSecretNoTx(userId string) (*string, error) {
	// This can simply call your existing GetTempSecret if you pass nil,
	// or just run a quick stand-alone query.
	var secret string
	err := r.database.QueryRow("SELECT temp_secret FROM users WHERE id = $1", userId).Scan(&secret)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}
