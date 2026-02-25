Please work on all the APPROVED items in the ROADMAP.md document, committing each as you go.

Ensure all tests pass, add additional test cases and regression tests as needed.

Follow these guidelines:

1. Read ROADMAP.md to identify all APPROVED work items
2. For each approved item:
   - Read the relevant source files before making changes
   - Implement the change as described in the roadmap
   - Run `go build ./...` and `go test -race -count=1 ./...` after each change
   - Commit the change with a clear, descriptive commit message
3. After all items are implemented:
   - Add regression tests for each change
   - Run `go test -race -coverprofile=coverage.out -covermode=atomic ./...` to verify coverage
   - Run `go vet ./...` to verify no static analysis issues
   - Commit the tests
4. Update ROADMAP.md:
   - Move completed items from APPROVED to COMPLETED with implementation notes
   - Update the test count and coverage percentage in the STATUS section
   - Commit the ROADMAP.md update
5. Push all commits to the current branch
