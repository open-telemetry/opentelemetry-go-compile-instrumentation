package main
import "fmt"
func main() {
    var val interface{} = nil
    var target error
    // Type assertion of nil interface to interface type
    err, ok := val.(error)
    fmt.Printf("err: %v, ok: %v\n", err, ok)
    
    // Direct assignment
    target = err
    fmt.Printf("target: %v\n", target)
    
    // But what if it's a pointer to a struct?
    var p *int
    // p, ok = val.(*int)
    // fmt.Printf("p: %v, ok: %v\n", p, ok)
}
