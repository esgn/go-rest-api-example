# Go REST API Example

A small Go REST API for notes, built from an OpenAPI-first workflow (`oapi-codegen`) with SQLite persistence.

## API Docs (Swagger)

Interactive Swagger UI is published on GitHub Pages:  
https://esgn.github.io/go-rest-api-example/

## Quick Start

```bash
cd src
go run ./cmd/api
```

Default server address: `:8080`  
Default SQLite file: `./notes.db`

## More Information

For architecture and design details, see [ARCHITECTURE.md](./ARCHITECTURE.md).
