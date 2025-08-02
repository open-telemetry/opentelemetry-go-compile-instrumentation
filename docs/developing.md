# Developing Details

## Project Structure

See the [project structure](./api-design-and-project-structure.md).

## Error Handling

We use the `ex` package to handle errors as we want to include the stack trace
in the error message for better debugging.

Any error should be wrapped with `ex.Error` or `ex.Errorf` to include the stack trace.

```go
// Wrap an existing error and return to the caller
return ex.Error(err)
```

```go
// Wrap the existing error with an additional message and return it to the caller.
return ex.Errorf(err, "additional context %s", "some value")
```

```go
// Create a new error with an additional message
return ex.Errorf(nil, "additional context %s", "some value")
```

Exit the program with `ex.Fatal` or `ex.Fatalf`.

```go
// Exit the program with an error
ex.Fatal(err)
```

```go
// Exit the program with an error and an additional message
ex.Fatalf("additional context %s", "some value")
```
