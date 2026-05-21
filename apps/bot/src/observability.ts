import { diag, DiagConsoleLogger, DiagLogLevel } from "@opentelemetry/api";
import { NodeSDK, logs as sdkLogs, metrics as sdkMetrics } from "@opentelemetry/sdk-node";
import { getNodeAutoInstrumentations } from "@opentelemetry/auto-instrumentations-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-grpc";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-grpc";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-grpc";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { ATTR_SERVICE_NAME, ATTR_SERVICE_NAMESPACE } from "@opentelemetry/semantic-conventions";
import Pyroscope from "@pyroscope/nodejs";

const enabled = process.env.OBSERVABILITY_ENABLED === "true";
const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;

let sdk: NodeSDK | undefined;

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
            exporter: new OTLPMetricExporter({ url: endpoint }),
            exportIntervalMillis: 30_000,
        }),
        logRecordProcessor: new sdkLogs.BatchLogRecordProcessor(new OTLPLogExporter({ url: endpoint })),
        instrumentations: getNodeAutoInstrumentations({
            "@opentelemetry/instrumentation-fs": { enabled: false },
        }),
    });

    sdk.start();

    if (process.env.PYROSCOPE_SERVER_ADDRESS) {
        Pyroscope.init({
            appName: process.env.OTEL_SERVICE_NAME ?? "rustlens-bot",
            serverAddress: process.env.PYROSCOPE_SERVER_ADDRESS,
            tags: {
                environment: process.env.DEPLOYMENT_ENV ?? "local",
                namespace: process.env.OTEL_SERVICE_NAMESPACE ?? "rustlens",
            },
        });
        Pyroscope.start();
    }

    return async () => {
        if (sdk) {
            await sdk.shutdown();
        }
        if (process.env.PYROSCOPE_SERVER_ADDRESS) {
            await Pyroscope.stop();
        }
    };
}
