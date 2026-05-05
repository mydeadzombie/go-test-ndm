PORT ?= 8080
BIN  := broker

.PHONY: build run test clean

build:
	go build -o $(BIN) .

run: build
	./$(BIN) $(PORT)

clean:
	rm -f $(BIN)

test: build
	@echo "→ starting broker on :$(PORT)"
	@./$(BIN) $(PORT) & echo $$! > .broker.pid; \
	trap 'kill $$(cat .broker.pid) 2>/dev/null; rm -f .broker.pid' EXIT; \
	set -e; \
	U=http://127.0.0.1:$(PORT); \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		curl -s -o /dev/null $$U/.ready && break; sleep 0.1; \
		[ $$i = 20 ] && { echo "  FAIL broker did not start"; exit 1; }; \
	done; \
	check() { got="$$1"; want="$$2"; label="$$3"; \
		if [ "$$got" = "$$want" ]; then echo "  ok   $$label ($$got)"; \
		else echo "  FAIL $$label: got '$$got' want '$$want'"; exit 1; fi; }; \
	echo "→ PUT pet=cat,dog / role=manager,executive"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' -XPUT $$U/pet?v=cat)"       200 "PUT /pet?v=cat"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' -XPUT $$U/pet?v=dog)"       200 "PUT /pet?v=dog"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' -XPUT $$U/role?v=manager)"  200 "PUT /role?v=manager"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' -XPUT $$U/role?v=executive)" 200 "PUT /role?v=executive"; \
	echo "→ GET FIFO drain"; \
	check "$$(curl -s $$U/pet)"  cat       "GET /pet #1"; \
	check "$$(curl -s $$U/pet)"  dog       "GET /pet #2"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' $$U/pet)" 404 "GET /pet empty"; \
	check "$$(curl -s $$U/role)" manager   "GET /role #1"; \
	check "$$(curl -s $$U/role)" executive "GET /role #2"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' $$U/role)" 404 "GET /role empty"; \
	echo "→ PUT without v"; \
	check "$$(curl -s -o /dev/null -w '%{http_code}' -XPUT $$U/x)" 400 "PUT /x (no v)"; \
	echo "→ GET timeout (no msg)"; \
	t0=$$(date +%s); code=$$(curl -s -o /dev/null -w '%{http_code}' "$$U/empty?timeout=3"); t1=$$(date +%s); \
	check "$$code" 404 "GET /empty?timeout=3"; \
	[ $$((t1-t0)) -ge 1 ] && echo "  ok   waited ~$$((t1-t0))s" || { echo "  FAIL did not wait"; exit 1; }; \
	echo "→ waiter then PUT"; \
	(curl -s "$$U/role?timeout=5" > .out1) & w=$$!; sleep 0.2; \
	curl -s -o /dev/null -XPUT "$$U/role?v=ceo"; wait $$w; \
	check "$$(cat .out1)" ceo "waiter received PUT"; rm -f .out1; \
	echo "→ two waiters FIFO order"; \
	(curl -s "$$U/q?timeout=5" > .a) & a=$$!; sleep 0.15; \
	(curl -s "$$U/q?timeout=5" > .b) & b=$$!; sleep 0.15; \
	curl -s -o /dev/null -XPUT "$$U/q?v=first"; \
	curl -s -o /dev/null -XPUT "$$U/q?v=second"; \
	wait $$a $$b; \
	check "$$(cat .a)" first  "first waiter"; \
	check "$$(cat .b)" second "second waiter"; rm -f .a .b; \
	echo "✓ all tests passed"
