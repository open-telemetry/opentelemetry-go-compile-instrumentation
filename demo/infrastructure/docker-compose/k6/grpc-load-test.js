import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const successRate = new Rate('success');
const requestDuration = new Trend('request_duration');

// Test configuration - Continuous waves pattern
export const options = {
  // Continuous load with periodic waves
  stages: [
    { duration: '2m', target: 10 },   // Baseline - 10 VUs
    { duration: '1m', target: 25 },   // Wave up to 25 VUs
    { duration: '2m', target: 25 },   // Sustain at 25 VUs
    { duration: '1m', target: 10 },   // Wave down to 10 VUs
    { duration: '2m', target: 10 },   // Baseline - 10 VUs
    { duration: '1m', target: 30 },   // Spike to 30 VUs
    { duration: '1m', target: 10 },   // Back to baseline
    { duration: '3m', target: 10 },   // Long baseline period
    // Pattern repeats - use Ctrl+C to stop or set --duration flag
  ],

  // Thresholds - define SLOs
  thresholds: {
    'grpc_req_duration': ['p(95)<300', 'p(99)<500'], // gRPC should be fast
    'errors': ['rate<0.05'],                          // Error rate below 5%
    'success': ['rate>0.95'],                         // Success rate above 95%
  },

  // Additional options
  noConnectionReuse: false,
};

// gRPC client
const client = new grpc.Client();

// Load proto file - mounted from demo/grpc/server/greeter.proto
// This ensures k6 always uses the same proto definition as the server
client.load(['/proto'], 'greeter.proto');

// Base URL - using Docker service name
const GRPC_SERVER = 'grpc-server:50051';

// Main test function - runs for each virtual user iteration
export default function () {
  // Connect to gRPC server
  client.connect(GRPC_SERVER, {
    plaintext: true, // Use insecure connection for demo
    timeout: '10s',
  });

  // Prepare request with unique name for each virtual user
  const request = {
    name: `k6-user-${__VU}-${Date.now()}`,  // VU = Virtual User
  };

  const startTime = new Date();

  try {
    // Call the gRPC method: greeter.Greeter/SayHello
    const response = client.invoke('greeter.Greeter/SayHello', request);

    const duration = new Date() - startTime;
    requestDuration.add(duration);

    // Check response
    const success = check(response, {
      'status is OK': (r) => r && r.status === grpc.StatusOK,
      'response has message': (r) => r && r.message && r.message.message !== undefined,
      'response time < 500ms': () => duration < 500,
      'message contains Hello': (r) => r && r.message && r.message.message.includes('Hello'),
    });

    errorRate.add(!success);
    successRate.add(success);

    if (!success) {
      console.error(`gRPC request failed: ${response.error ? response.error.message : 'unknown error'}`);
    } else if (__ITER % 100 === 0) {
      // Log every 100th successful request to avoid log spam
      console.log(`[VU ${__VU}] Received: ${response.message.message} (${duration}ms)`);
    }

  } catch (error) {
    console.error(`gRPC error: ${error}`);
    errorRate.add(1);
    successRate.add(0);
  } finally {
    client.close();
  }

  // Think time - simulate real user behavior
  // Random sleep between 1-3 seconds
  sleep(Math.random() * 2 + 1);
}

// Setup function - runs once at the beginning
export function setup() {
  console.log('='.repeat(60));
  console.log('Starting gRPC load test');
  console.log(`Target: ${GRPC_SERVER}`);
  console.log(`Service: greeter.Greeter`);
  console.log(`Method: SayHello`);
  console.log('Pattern: Continuous waves with periodic scaling');
  console.log('Press Ctrl+C to stop the test');
  console.log('='.repeat(60));

  return { startTime: new Date() };
}

// Teardown function - runs once at the end
export function teardown(data) {
  const endTime = new Date();
  const duration = (endTime - data.startTime) / 1000;
  console.log('='.repeat(60));
  console.log(`Test completed. Duration: ${duration.toFixed(2)}s`);
  console.log('='.repeat(60));
}

// Handle summary - custom summary output
export function handleSummary(data) {
  return {
    'stdout': JSON.stringify(data, null, 2),
    '/tmp/k6-grpc-summary.json': JSON.stringify(data),
  };
}
