package user

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

func (s *Server) GetAllClients(firstName string, lastName string, email string) ([]Client, error) {
	query := `SELECT id, first_name, last_name, date_of_birth, gender, email, phone_number, address FROM clients`
	var conditions []string
	var args []interface{}

	if firstName != "" {
		conditions = append(conditions, "first_name = $"+strconv.Itoa(len(args)+1))
		args = append(args, firstName)
	}
	if lastName != "" {
		conditions = append(conditions, "last_name = $"+strconv.Itoa(len(args)+1))
		args = append(args, lastName)
	}
	if email != "" {
		conditions = append(conditions, "email = $"+strconv.Itoa(len(args)+1))
		args = append(args, email)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_name ASC, first_name ASC"

	rows, err := s.database.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing clients: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var clients []Client
	for rows.Next() {
		client, err := scanClient(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning client: %w", err)
		}
		clients = append(clients, *client)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating clients: %w", err)
	}
	return clients, nil
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
