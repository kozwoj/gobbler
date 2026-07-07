module github.com/kozwoj/gobbler

go 1.24.1

require (
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.6.4
	github.com/go-chi/chi/v5 v5.2.5
	github.com/kozwoj/gobbler-client v0.1.0
	github.com/kozwoj/gobbler-query v0.0.0-20260705193305-e3c2db092cf7
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.20.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/text v0.31.0 // indirect
)

replace github.com/kozwoj/gobbler-query => ../gobbler-query
