import { diag, DiagConsoleLogger, DiagLogLevel } from "@opentelemetry/api";
import { NodeSDK, logs as sdkLogs, metrics as sdkMetrics } from "@opentelemetry/sdk-node";
import { getNodeAutoInstrumentations } from "@opentelemetry/auto-instrumentations-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-grpc";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-grpc";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-grpc";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { ATTR_SERVICE_NAME, ATTR_SERVICE_NAMESPACE } from "@opentelemetry/semantic-conventions";

const { AggregationTemporality } = sdkMetrics;

const enabled = process.env.OBSERVABILITY_ENABLED === "true";
const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;

let sdk: NodeSDK | undefined;
let pyroscopeStarted = false;

export async function startObservability(): Promise<() => Promise<void>> {
    if (!enabled) {
        return async () => undefined;
    }
    if (!endpoint) {
        throw new Error("OBSERVABILITY_ENABLED=true but OTEL_EXPORTER_OTLP_ENDPOINT is empty");
    }

    diag.setLogger(new DiagConsoleLogger(), DiagLogLevel.WARN);

    const resource = resourceFromAttributes({
        [ATTR_SERVICE_NAME]: process.env.OTEL_SERVICE_NAME ?? "rustlens-bot",
        [ATTR_SERVICE_NAMESPACE]: process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
        "deployment.environment": process.env.DEPLOYMENT_ENV ?? "local",
        team: "rustlens",
    });

    sdk = new NodeSDK({
        resource,
        traceExporter: new OTLPTraceExporter({ url: endpoint }),
        metricReader: new sdkMetrics.PeriodicExportingMetricReader({
            // Pin every instrument kind to cumulative temporality. The SDK's
            // default selector returns delta for some kinds; Mimir's OTLP
            // endpoint then mis-handles the counter-reset semantics and
            // inflates rate()/increase() by 50–200x.
            exporter: new OTLPMetricExporter({
                url: endpoint,
                temporalityPreference: AggregationTemporality.CUMULATIVE,
            }),
            exportIntervalMillis: 30_000,
        }),
        logRecordProcessor: new sdkLogs.BatchLogRecordProcessor(new OTLPLogExporter({ url: endpoint })),
        instrumentations: getNodeAutoInstrumentations({
            "@opentelemetry/instrumentation-fs": { enabled: false },
        }),
    });

    sdk.start();

    // Pyroscope is best-effort. Its native dep (@datadog/pprof) only ships
    // glibc binaries, so on Alpine/musl the module-load throws synchronously.
    // Lazy import + try/catch lets the rest of observability come up even when
    // profiling can't.
    if (process.env.PYROSCOPE_SERVER_ADDRESS) {
        try {
            const { default: Pyroscope } = await import("@pyroscope/nodejs");
            Pyroscope.init({
                appName: process.env.OTEL_SERVICE_NAME ?? "rustlens-bot",
                serverAddress: process.env.PYROSCOPE_SERVER_ADDRESS,
                tags: {
                    environment: process.env.DEPLOYMENT_ENV ?? "local",
                    namespace: process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
                },
            });
            Pyroscope.start();
            pyroscopeStarted = true;
        } catch (err) {
            console.warn(
                "[observability] pyroscope unavailable, continuing without profiling:",
                err instanceof Error ? err.message : String(err),
            );
        }
    }

    return async () => {
        if (sdk) {
            await sdk.shutdown();
        }
        if (pyroscopeStarted) {
            const { default: Pyroscope } = await import("@pyroscope/nodejs");
            await Pyroscope.stop();
        }
    };
}
