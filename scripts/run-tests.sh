#!/bin/bash
set -e

# Create test result directory
mkdir -p test-result

# Run tests with coverage
echo "Running all tests..."
go test -v ./internal/... -coverprofile=test-result/coverage.out 2>&1 | tee test-result/test-output.txt

# Generate coverage report
go tool cover -func=test-result/coverage.out > test-result/coverage-report.txt
echo "Coverage report generated at test-result/coverage-report.txt"

# Analyze results
FAILED_TESTS=$(grep "FAIL" test-result/test-output.txt | grep -v "FAIL\t" || true)
if [ -n "$FAILED_TESTS" ]; then
    echo "Some tests failed:"
    echo "$FAILED_TESTS"
    exit 1
else
    echo "All tests passed!"
fi
