module github.com/RAF-SI-2025/Banka-3-Backend/services/exchange

go 1.25.0

replace github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto => ../../pkg/proto

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.9.1
	github.com/stretchr/testify v1.11.1
	google.golang.org/grpc v1.79.3
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
