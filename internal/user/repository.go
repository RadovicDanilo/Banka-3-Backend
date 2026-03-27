package user

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type User struct {
	email          string
	hashedPassword []byte
	salt           []byte
}

type (

	// I am indeed aware (unlike most)
	// That these are in fact the same type
	// But We should disambiguate what purpose
	// these are used for
	user_restrictions = map[string]string
	user_updates      = map[string]string
)

var ErrInvalidPasswordActionToken = errors.New("invalid or expired password token")

var ErrClientNotFound = errors.New("client not found")
var ErrClientEmailExists = errors.New("client email already exists")
var ErrClientNoFieldsToUpdate = errors.New("no client fields to update")
var ErrEmployeeNotFound = errors.New("employee not found")

var ErrUnknownPermission = errors.New("unknown permissions")

func (s *Server) GetUserByEmail(email string) (*User, error) {
	query := `
		SELECT email, password, salt_password FROM employees WHERE email = $1
		UNION ALL
		SELECT email, password, salt_password FROM clients WHERE email = $1
		LIMIT 1
	`

	var user User

	err := s.database.QueryRow(query, email).Scan(&user.email, &user.hashedPassword, &user.salt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Server) rotateRefreshToken(tx *sql.Tx, email string, oldHash, newHash []byte, newExpiry time.Time) error {
	var storedHash []byte
	err := tx.QueryRow(`
        SELECT hashed_token FROM refresh_tokens
        WHERE email = $1 AND revoked = FALSE AND valid_until > now()
        FOR UPDATE
    `, email).Scan(&storedHash)
	if err != nil {
		return fmt.Errorf("refresh token not found or expired: %w", err)
	}

	if !bytes.Equal(storedHash, oldHash) {
		_, err := tx.Exec(`UPDATE refresh_tokens SET revoked = TRUE WHERE email = $1`, email)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to revoke tokens: %w", err)
		}
		_ = tx.Commit()
		return fmt.Errorf("token mismatch: possible reuse attack")
	}

	_, err = tx.Exec(`
        UPDATE refresh_tokens
        SET hashed_token = $1, valid_until = $2, revoked = FALSE
        WHERE email = $3
    `, newHash, newExpiry, email)
	return err
}

func (s *Server) InsertRefreshToken(token string) error {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, &jwt.RegisteredClaims{})
	if err != nil {
		return fmt.Errorf("parsing token: %w", err)
	}

	email, err := parsed.Claims.GetSubject()
	if err != nil {
		return fmt.Errorf("getting subject: %w", err)
	}

	expiry, err := parsed.Claims.GetExpirationTime()
	if err != nil {
		return fmt.Errorf("getting expiry: %w", err)
	}
	hasher := sha256.New()
	hasher.Write([]byte(token))
	hashed_token := hasher.Sum(nil)
	query := `
	INSERT INTO refresh_tokens VALUES ($1, $2, $3, FALSE)
	ON CONFLICT (email) DO UPDATE SET (hashed_token, valid_until, revoked) = (excluded.hashed_token, excluded.valid_until, excluded.revoked)
	`
	_, err = s.database.Exec(query, email, hashed_token, expiry.Time)
	if err != nil {
		return fmt.Errorf("inserting refresh token: %w", err)
	}

	return nil
}

func (s *Server) UpsertPasswordActionToken(email, actionType string, hashedToken []byte, validUntil time.Time) error {
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

	_, err := s.database.Exec(query, email, actionType, hashedToken, validUntil)
	if err != nil {
		return fmt.Errorf("upserting password action token: %w", err)
	}
	return nil
}

func (s *Server) ConsumePasswordActionToken(tx *sql.Tx, hashedToken []byte) (string, string, error) {
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

func (s *Server) UpdatePasswordByEmail(tx *sql.Tx, email string, hashedPassword []byte) error {
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

func (s *Server) RevokeRefreshTokensByEmail(tx *sql.Tx, email string) error {
	_, err := tx.Exec(`UPDATE refresh_tokens SET revoked = TRUE WHERE email = $1`, email)
	if err != nil {
		return fmt.Errorf("revoking refresh tokens: %w", err)
	}
	return nil
}

func scanClient(scanner interface {
	Scan(dest ...any) error
}) (*Client, error) {
	var client Client
	err := scanner.Scan(
		&client.Id,
		&client.First_name,
		&client.Last_name,
		&client.Date_of_birth,
		&client.Gender,
		&client.Email,
		&client.Phone_number,
		&client.Address,
	)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (s *Server) GetClientByID(id int64) (*Client, error) {
	row := s.database.QueryRow(`
		SELECT id, first_name, last_name, date_of_birth, gender, email, phone_number, address
		FROM clients
		WHERE id = $1
	`, id)

	client, err := scanClient(row)
	if err == sql.ErrNoRows {
		return nil, ErrClientNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting client by id: %w", err)
	}

	return client, nil
}

func GetAllUsersFromModel[T Client | Employee](user T, s *Server, constraints user_restrictions) ([]T, error) {
	add_constraints := func(query *gorm.DB, restrictions user_restrictions) *gorm.DB {
		for _, key := range restrictions {
			if restrictions[key] != "" {
				switch key {
				case "email", "position":
					query = query.Where(key+"= ?", restrictions[key])
				default:
					query = query.Where(key+"ILIKE ?", "%"+restrictions[key]+"%")
				}
			}
		}
		return query
	}
	// Me when I don't have template specialization frfr
	switch any(user).(type) {
	case Client:
		var clients []T
		query := s.db_gorm.Model(&Client{})
		query = add_constraints(query, constraints)
		err := query.Find(&clients).Error
		if err != nil {
			return nil, err
		}
		return clients, nil

	case Employee:
		var employees []T
		query := s.db_gorm.Model(&Employee{}).Preload("Permissions")
		query = add_constraints(query, constraints)
		err := query.Find(&employees).Error
		if err != nil {
			return nil, err
		}
		return employees, nil
	}
	return nil, fmt.Errorf("Called with a type which is neither Client nor employee")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func create_user_from_model[T Client | Employee](user T, s *Server) error {
	result := s.db_gorm.Create(&user)
	if result.Error != nil {
		log.Printf("We got this error: %s", result.Error.Error())
		return result.Error
	}
	return nil
}

func getUserByAttribute[T Client | Employee](user T, s *Server, attribute_name string, attribute_value any) (*T, error) {
	var ret T
	err := s.db_gorm.Preload("Permissions").Where(attribute_name+"= ?", attribute_value).First(&ret).Error
	if err != nil {
		log.Println("Error from getEmployeeByAttribute: ", err)
		return nil, err
	}

	log.Println(ret)
	return &ret, nil
}

func (s *Server) deleteEmployee(id int64) error {
	resp := s.db_gorm.Delete(&Employee{}, id)
	if resp.RowsAffected == 0 {
		return ErrEmployeeNotFound
	}
	return nil
}

func (s *Server) UpdateClientRecord(client *Client) error {
	updates := map[string]any{}

	if strings.TrimSpace(client.First_name) != "" {
		updates["first_name"] = strings.TrimSpace(client.First_name)
	}
	if strings.TrimSpace(client.Last_name) != "" {
		updates["last_name"] = strings.TrimSpace(client.Last_name)
	}
	if !client.Date_of_birth.IsZero() {
		updates["date_of_birth"] = client.Date_of_birth
	}
	if strings.TrimSpace(client.Gender) != "" {
		updates["gender"] = strings.TrimSpace(client.Gender)
	}
	if strings.TrimSpace(client.Email) != "" {
		updates["email"] = strings.TrimSpace(client.Email)
	}
	if strings.TrimSpace(client.Phone_number) != "" {
		updates["phone_number"] = strings.TrimSpace(client.Phone_number)
	}
	if strings.TrimSpace(client.Address) != "" {
		updates["address"] = strings.TrimSpace(client.Address)
	}

	if len(updates) == 0 {
		return ErrClientNoFieldsToUpdate
	}

	updates["updated_at"] = time.Now()

	result := s.db_gorm.Model(&Client{}).Where("id = ?", client.Id).Updates(updates)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return ErrClientEmailExists
		}
		return fmt.Errorf("updating client: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrClientNotFound
	}

	return nil
}

// Try to fix this once you understand gorm
func (s *Server) UpdateEmployee_(emp *Employee) (*Employee, error) {

	updates := map[string]any{
		"first_name":   emp.First_name,
		"last_name":    emp.Last_name,
		"gender":       emp.Gender,
		"phone_number": emp.Phone_number,
		"address":      emp.Address,
		"position":     emp.Position,
		"department":   emp.Department,
		"active":       emp.Active,
	}

	tx := s.db_gorm.Begin()

	if err := tx.Model(&Employee{}).
		Where("id = ?", emp.Id).
		Updates(updates).Error; err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	var perms []Permission
	var names []string

	for _, p := range emp.Permissions {
		names = append(names, p.Name)
	}

	if err := tx.
		Where("name IN ?", names).
		Find(&perms).Error; err != nil {
		tx.Rollback()
		return nil, ErrUnknownPermission
	}

	if err := tx.Model(emp).
		Association("Permissions").
		Replace(&perms); err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	var updated Employee
	if err := tx.
		Preload("Permissions").
		First(&updated, emp.Id).Error; err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return &updated, nil
}

var ErrUserNotFound = errors.New("user not found")

func (s *TOTPServer) getUserIdByEmail(email string) (*uint64, error) {
	query := `
		SELECT id FROM employees WHERE email = $1
		UNION ALL
		SELECT id FROM clients WHERE email = $1
		LIMIT 1
	`

	var id uint64

	err := s.db.QueryRow(query, email).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *TOTPServer) SetTempTOTPSecret(id uint64, secret string) error {
	_, err := s.db.Exec(`
		INSERT INTO verification_codes (client_id, temp_secret, temp_created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (client_id)
		DO UPDATE SET
			temp_secret = EXCLUDED.temp_secret,
			temp_created_at = NOW()
	`, id, secret)
	return err
}

func (s *TOTPServer) GetTempSecret(tx *sql.Tx, id uint64) (*string, error) {
	var temp_secret string
	row := tx.QueryRow(`
		SELECT temp_secret
		FROM verification_codes
		WHERE client_id = $1
		FOR UPDATE
	`, id)
	if row == nil {
		return nil, ErrUserNotFound
	}
	err := row.Scan(&temp_secret)
	if err != nil {
		return nil, err
	}
	return &temp_secret, nil
}

func (s *TOTPServer) GetSecret(id uint64) (*string, error) {
	var secret string
	row := s.db.QueryRow(`
		SELECT secret
		FROM verification_codes
		WHERE client_id = $1 AND enabled = TRUE
	`, id)
	err := row.Scan(&secret)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return &secret, nil
}

func (s *TOTPServer) EnableTOTP(tx *sql.Tx, id uint64, tempSecret string) error {
	_, err := tx.Exec(`
		UPDATE verification_codes
		SET enabled = TRUE,
		    secret = $1,
		    temp_secret = NULL
		WHERE client_id = $2
	`, tempSecret, id)

	if err != nil {
		return err
	}
	return nil
}
