module github.com/RAF-SI-2025/Banka-3-Backend/services/notification

go 1.25.0

replace github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto => ../../pkg/proto

require (
	github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.79.3
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
