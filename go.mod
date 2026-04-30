module github.com/kozwoj/gobbler

go 1.24.1

require github.com/kozwoj/gobbler-client v0.0.0

replace github.com/kozwoj/gobbler-client => ./client

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.20.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.6.4 // indirect
	github.com/go-chi/chi/v5 v5.2.5 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/text v0.31.0 // indirect
)
