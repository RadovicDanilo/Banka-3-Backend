package gateway

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type getEmployeeByIDURI struct {
	EmployeeID int64 `uri:"id" binding:"required"`
}

type passwordResetRequestRequest struct {
	Email string `json:"email" binding:"required"`
}

type passwordResetConfirmationRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"password" binding:"required"`
}

type createClientAccountRequest struct {
	FirstName   string `json:"first_name" binding:"required"`
	LastName    string `json:"last_name" binding:"required"`
	DateOfBirth int64  `json:"date_of_birth"`
	Gender      string `json:"gender"`
	Email       string `json:"email" binding:"required"`
	PhoneNumber string `json:"phone_number"`
	Address     string `json:"address"`
	Password    string `json:"password"`
}

type createEmployeeAccountRequest struct {
	FirstName   string `json:"first_name" binding:"required"`
	LastName    string `json:"last_name" binding:"required"`
	DateOfBirth int64  `json:"date_of_birth"`
	Gender      string `json:"gender"`
	Email       string `json:"email" binding:"required"`
	PhoneNumber string `json:"phone_number"`
	Address     string `json:"address"`
	Username    string `json:"username" binding:"required"`
	Position    string `json:"position"`
	Department  string `json:"department"`
	Password    string `json:"password"`
}

type createLoanRequestRequest struct {
	AccountNumber   string  `json:"account_number" binding:"required"`
	LoanType        string  `json:"loan_type" binding:"required"`
	Amount          float64 `json:"amount" binding:"required"`
	RepaymentPeriod int32   `json:"repayment_period" binding:"required"`
	Currency        string  `json:"currency" binding:"required"`
}

type getLoansQuery struct {
	LoanType      string `form:"loan_type"`
	AccountNumber string `form:"account_number"`
	Status        string `form:"status"`
}

type getLoanByNumberURI struct {
	LoanNumber string `uri:"loanNumber" binding:"required"`
}
