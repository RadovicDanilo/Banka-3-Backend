package user

func (s *Server) getEmployeeByEmail(email string) (*Employee, error) {
	var employee Employee
	err := s.db_gorm.Preload("Permissions").Where("email = ?", email).First(&employee).Error
	if err != nil {
		return nil, err
	}
	for _, perm := range employee.Permissions {
		println(perm.Name)
	}
	return &employee, nil
}

func (s *Server) getEmployeeById(id int64) (*Employee, error) {
	var employee Employee
	err := s.db_gorm.Preload("Permissions").Where("id = ?", id).First(&employee).Error
	if err != nil {
		return nil, err
	}
	for _, perm := range employee.Permissions {
		println(perm.Name)
	}
	return &employee, nil
}

func (s *Server) deleteEmployee(id int64) error {
	resp := s.db_gorm.Delete(&Employee{}, id)
	if resp.RowsAffected == 0 {
		return ErrEmployeeNotFound
	}
	return nil
}

func (s *Server) GetAllEmployees(email *string, name *string, lastName *string, position *string) ([]Employee, error) {
	var employees []Employee
	query := s.db_gorm.Model(&Employee{}).Preload("Permissions")

	if email != nil && *email != "" {
		query = query.Where("email = ?", *email)
	}
	if name != nil && *name != "" {
		query = query.Where("first_name ILIKE ?", "%"+*name+"%")
	}
	if lastName != nil && *lastName != "" {
		query = query.Where("last_name ILIKE ?", "%"+*lastName+"%")
	}
	if position != nil && *position != "" {
		query = query.Where("position = ?", *position)
	}

	query = query.Where("active = true")
	err := query.Find(&employees).Error
	if err != nil {
		return nil, err
	}
	return employees, nil
}

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
	if err := tx.Model(&Employee{}).Where("id = ?", emp.Id).Updates(updates).Error; err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	var perms []Permission
	var names []string
	for _, p := range emp.Permissions {
		names = append(names, p.Name)
	}

	if err := tx.Where("name IN ?", names).Find(&perms).Error; err != nil {
		tx.Rollback()
		return nil, ErrUnknownPermission
	}

	if err := tx.Model(emp).Association("Permissions").Replace(&perms); err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	var updated Employee
	if err := tx.Preload("Permissions").First(&updated, emp.Id).Error; err != nil {
		tx.Rollback()
		return nil, ErrEmployeeNotFound
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return &updated, nil
}
