MYSQL_DSN ?= bank:bank@tcp(localhost:3306)/bank?parseTime=true&loc=UTC
PYTEST    := .venv/bin/python -m pytest
ALLURE    := npx --yes allure@3.19.1
K6        := docker compose run --rm k6 run --summary-trend-stats="avg,med,p(90),p(95),p(99),max"

.PHONY: help up down logs test-unit test-integration coverage-html venv test-api test-bdd test-smoke test-python report perf-smoke perf-load perf-stress perf-spike

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

up: ## Build and start api + mysql + redis, waiting until all are healthy
	docker compose up -d --build --wait

down: ## Stop everything and discard data
	docker compose down -v

logs: ## Tail api logs
	docker compose logs -f api

test-unit: ## Go unit + handler tests with race detector and coverage (no Docker needed)
	cd service && go test -race -coverprofile=coverage.out -coverpkg=./internal/... ./...
	cd service && go tool cover -func=coverage.out | tail -1

test-integration: ## Go store tests against the docker-compose MySQL (run `make up` first)
	cd service && MYSQL_DSN='$(MYSQL_DSN)' go test -race -count=1 -tags integration ./internal/store/mysqlstore/ -v

coverage-html: test-unit ## Open the coverage report in a browser
	cd service && go tool cover -html=coverage.out

venv: tests/.venv/.installed ## Create the Python virtualenv and install test dependencies

tests/.venv/.installed: tests/requirements.txt
	python3 -m venv tests/.venv
	tests/.venv/bin/pip install -q -r tests/requirements.txt
	touch $@

test-api: venv ## Python API tests against the running stack (run `make up` first)
	cd tests && $(PYTEST) api

test-bdd: venv ## pytest-bdd scenarios against the running stack
	cd tests && $(PYTEST) bdd

test-smoke: venv ## Only the @smoke tests: a quick health check of any environment
	cd tests && $(PYTEST) -m smoke

test-python: venv ## API + BDD tests in one run (one combined Allure report)
	cd tests && $(PYTEST)

report: ## Build the Allure HTML report from the last pytest run and open it
	cd tests && $(ALLURE) generate allure-results && $(ALLURE) open allure-report

perf-smoke: ## k6 smoke test (30s, 5 VUs): the CI quality gate
	$(K6) --summary-export=results/smoke-summary.json smoke.js

perf-load: ## k6 load test: ramp to 50 VUs, hold 3 min (~5 min)
	$(K6) --summary-export=results/load-summary.json load.js

perf-stress: ## k6 stress test: step the request rate up until SLOs break (~6 min)
	$(K6) --summary-export=results/stress-summary.json stress.js

perf-spike: ## k6 spike test: 10 -> 300 VUs and back, checks recovery (~3 min)
	$(K6) --summary-export=results/spike-summary.json spike.js
