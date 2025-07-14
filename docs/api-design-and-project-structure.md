# API Design & Project Structure

The project structure is as follows:

```
internal ---> Compile-Time-Instrumentation tools
demo ---> Demo Application
pkg ---> Public API
    inst-api ---> Encapsulation of instrumentation (generating span, metrics, ...)
        instrumenter
    inst-api-semconv ---> Encapsulation of OpenTelemetry SemConv
        instrumenter
            http
            rpc
            db
            messaging
            network
sdk ---> Instrumentation code for each plugin (e.g. http, grpc, ...)
```

For Public API, we have some key abstractions as follows:

1. `Instrumenter`: Unified entrance of instrumentation (generating span, metrics, ...)
2. `Extractor`: Extracting attributes using multiple Getters according to [OpenTelemetry Semconv](https://opentelemetry.io/docs/specs/semconv/).
3. `Getter`: Getting attributes from the REQUEST object(For example, HTTPRequest object that HTTP Server received).
4. `AttrsShadower`: An extension for Extractor to customize the attributes extracted by the extractor.
5. `OperationListener`: An hook for better extension(For example, aggregate the metrics).

And the relationship between these key components is shown below:

```mermaid
classDiagram
    direction RL

    namespace Instrumenters {
        class Instrumenter {
            <<Interface>>
            + start(Context, REQUEST) Context
            + shouldStart(Context, REQUEST)
            + end(Context, REQUEST, RESPONSE, Error)
        }
        class PropagatingFromUpstreamInstrumenter {
            + start(Context, REQUEST)
            + shouldStart(Context, REQUEST)
            + end(Context, REQUEST, RESPONSE, Error)
        }
        class PropagatingFromDownstreamInstrumenter  {
            + start(Context, REQUEST)
            + shouldStart(Context, REQUEST)
            + end(Context, REQUEST, RESPONSE, Error)
        }
    }

    namespace Getters {
        class AttributesGetter {
            <<Interface>>
            + GetXxx(REQUEST) string
            + ...(...)
        }
        class HTTPCommonAttributesGetter {
            + GetRequestMethod(REQUEST) string
            + ...(...)
        }
        class RcpAttributesGetter {
            + GetSystem(REQUEST) string
            + ...(...)
        }
        class MessagingAttributesGetter {
            + GetDestination(REQUEST) string
            + ...(...)
        }
    }

    HTTPCommonAttributesGetter ..|> AttributesGetter
    RcpAttributesGetter ..|> AttributesGetter
    MessagingAttributesGetter ..|> AttributesGetter

    class AttributesExtractor {
        <<Interface>>
        + onStart(AttributesBuilder, Context, REQUEST)
        + onEnd(AttributesBuyilder, Context, REQUEST, RESPONSE, Error)
    }
    AttributesExtractor *-- "1..*" AttributesGetter
    AttributesExtractor *-- "1..*" AttrsShadower

    namespace Shadowers {
        class AttrsShadower {
            <<Interface>>
            + Shadow([]KeyValue) (int, []KeyValue)
        }
        class NoopAttrsShadower {
            + Shadow([]KeyValue) (int, []KeyValue)
        }
        class XxxAttrsShadower {
            + Shadow([]KeyValue) (int, []KeyValue)
        }
    }

    AttrsShadower <|.. NoopAttrsShadower
    AttrsShadower <|.. XxxAttrsShadower

    namespace Extractors {
        class SpanNameExtractor {
            + Extract(REQUEST) string
        }
        class SpanKindExtractor {
            + Extract(REQUEST) string
        }
        class SpanStatusExtractor {
            + Extract(Span, REQUEST, RESPONSE, Error) string
        }
    }

    Instrumenter *-- "1..*" AttributesExtractor
    Instrumenter *-- "0..*" SpanNameExtractor
    Instrumenter *-- "0..*" SpanKindExtractor
    Instrumenter *-- "0..*" SpanStatusExtractor

    namespace Listeners {
        class OperationListener {
            <<Interface>>
            + OnBeforeStart(Context, Time) Context
            + OnBeforeEnd(Context, []KeyValue, Time) Context
            + OnAfterStart(Context, Time)
            + OnAfterEnd(Context, []KeyValue, Time)
        }
        class HTTPServerMetric {
            + OnBeforeStart(Context, Time) Context
            + OnBeforeEnd(Context, []KeyValue, Time) Context
            + OnAfterStart(Context, Time)
            + OnAfterEnd(Context, []KeyValue, Time)
        }
        class XxxOperationListener {
            + OnBeforeStart(Context, Time) Context
            + OnBeforeEnd(Context, []KeyValue, Time) Context
            + OnAfterStart(Context, Time)
            + OnAfterEnd(Context, []KeyValue, Time)
        }
    }

    Instrumenter *-- "1..*" OperationListener

    OperationListener <|.. HTTPServerMetric
    OperationListener <|.. XxxOperationListener

    PropagatingFromUpstreamInstrumenter ..|> Instrumenter
    PropagatingFromDownstreamInstrumenter ..|> Instrumenter
```

The `Instrumenter` will hold a number of `Extractor`s and `OperationListener`s, the `Extractor` will
hold a number of `Getter`s and `AttrsShadower`s. The `Getter` extracts the important attributes of
OpenTelemetry and returns them to the `Extractor`, which then makes the necessary cuts to these
attributes through the `AttrsShadower`. After `Instrumenter` gets the necessary attributes through
`Extractor`, it will perform some callback operations through `OperationListener`, such as
aggregation of metrics etc.

```mermaid
sequenceDiagram
    autonumber

    actor User as #160;
    activate User

    User ->>+ Instrumenter: Start
    Instrumenter ->>+ Extractor: Extract
    Extractor ->>+ Getter: Get
    Getter ->>- Extractor: #160;
    Extractor ->> Instrumenter: #160;
    Extractor ->>+ AttrsShadower: Shadow
    AttrsShadower ->>- Extractor: #160;
    Instrumenter ->>+ OperationListener: OnBeforeStart/OnAfterStart
    OperationListener ->>- Instrumenter: #160;
    Instrumenter ->>- User: #160;

    User -->> User: #160;
    deactivate User
```
