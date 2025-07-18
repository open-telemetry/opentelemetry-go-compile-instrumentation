# Developing Details


## Project Structure
See the [project structure](./api-design-and-project-structure.md).

## Error Handling

Any error should be wrapped with `ex.Error` or `ex.Errorf` to include the stack trace.

```go
ex.Error(err)
```

```go
ex.Errorf(err, "additional context %s", "some value")
```

Exit the program with `ex.Fatal` or `ex.Fatalf`.

```go
ex.Fatal(err)
```

```go
ex.Fatalf("additional context %s", "some value")
```
