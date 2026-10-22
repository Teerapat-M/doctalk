.PHONY: server-test server-build client-install client-test client-build compose-up

server-test:
	cd doctalk-server && go test ./...

server-build:
	cd doctalk-server && go build -o doctalk-server .

client-install:
	cd doctalk-client && npm ci

client-test:
	cd doctalk-client && npm test

client-build:
	cd doctalk-client && npm run build

compose-up:
	docker compose up --build
