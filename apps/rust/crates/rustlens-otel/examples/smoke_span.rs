//! Manual smoke check for `rustlens_otel::init`.
//!
//! Emits one root span (with a child span and an event) through whatever
//! pipeline `init` selects, then flushes on shutdown. Run it against a throwaway
//! OTLP collector to confirm spans arrive with the expected `service.name`:
//!
//! ```bash
//! OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 \
//! OTEL_SERVICE_NAME=rustlens-identity \
//!   cargo run -p rustlens-otel --example smoke_span
//! ```
//!
//! With `OTEL_EXPORTER_OTLP_ENDPOINT` unset it just prints the span/event to
//! stdout, demonstrating the dev fallback is unchanged.

use tracing::{info, info_span};

#[tokio::main]
async fn main() {
    let guard = rustlens_otel::init("rustlens-otel-smoke", "info");

    {
        let span = info_span!("smoke.request", component = "rustlens-otel", check = "d2");
        let _entered = span.enter();
        info!(step = "work", "doing smoke work inside the span");

        let child = info_span!("smoke.db_query", table = "app_users");
        let _child = child.enter();
        info!("child span active");
    }

    // Flush and shut the exporters down so the batch export is delivered before
    // the process exits.
    guard.shutdown();
}
