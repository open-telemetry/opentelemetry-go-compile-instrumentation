import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const successRate = new Rate('success');
const requestDuration = new Trend('request_duration');

// Test configuration
export const options = {
  // Load testing stages
  stages: [
    { duration: '30s', target: 10 },  // Ramp-up to 10 users
    { duration: '2m', target: 20 },   // Ramp-up to 20 users
    { duration: '3m', target: 20 },   // Stay at 20 users
    { duration: '1m', target: 50 },   // Spike to 50 users
    { duration: '2m', target: 20 },   // Scale back to 20 users
    { duration: '30s', target: 0 },   // Ramp-down to 0 users
  ],

  // Thresholds - define SLOs
  thresholds: {
    'grpc_req_duration': ['p(95)<300', 'p(99)<500'], // gRPC should be faster than HTTP
    'errors': ['rate<0.05'],                          // Error rate below 5%
    'success': ['rate>0.95'],                         // Success rate above 95%
  },

  // Additional options
  noConnectionReuse: false,
};

// gRPC client
const client = new grpc.Client();

// Base URL - using Docker service name
const GRPC_SERVER = 'grpc-server:50051';

// Main test function
export default function () {
  // Connect to gRPC server
  client.connect(GRPC_SERVER, {
    plaintext: true, // Use insecure connection for demo
    timeout: '10s',
  });

  // Example: Call a unary RPC method
  // Replace with your actual service and method names
  const request = {
    name: `user-${__VU}`,  // VU = Virtual User
    message: 'Hello from k6',
  };

  const startTime = new Date();

  try {
    // Replace 'YourService/YourMethod' with actual gRPC service/method
    // Example: const response = client.invoke('demo.GreetingService/SayHello', request);

    // For now, this is a template - uncomment and modify when gRPC demo is ready
    /*
    const response = client.invoke('demo.GreetingService/SayHello', request);

    const duration = new Date() - startTime;
    requestDuration.add(duration);

    // Check response
    const success = check(response, {
      'status is OK': (r) => r && r.status === grpc.StatusOK,
      'response has data': (r) => r && r.message !== undefined,
      'response time < 500ms': () => duration < 500,
    });

    errorRate.add(!success);
    successRate.add(success);

    if (!success) {
      console.error(`gRPC request failed: ${response.error ? response.error.message : 'unknown error'}`);
    }
    */

    // Placeholder for template
    console.log('gRPC load test template - update with actual service methods');
    sleep(1);

  } catch (error) {
    console.error(`gRPC error: ${error}`);
    errorRate.add(1);
    successRate.add(0);
  } finally {
    client.close();
  }

  // Think time - simulate real user behavior
  sleep(Math.random() * 2 + 1); // Random sleep between 1-3 seconds
}

// Setup function - runs once at the beginning
export function setup() {
  console.log('Starting gRPC load test');
  console.log(`Target: ${GRPC_SERVER}`);
  console.log('NOTE: This is a template - update with actual gRPC service definitions');

  return { startTime: new Date() };
}

// Teardown function - runs once at the end
export function teardown(data) {
  const endTime = new Date();
  const duration = (endTime - data.startTime) / 1000;
  console.log(`Test completed. Duration: ${duration.toFixed(2)}s`);
}

// Handle summary - custom summary output
export function handleSummary(data) {
  return {
    'stdout': JSON.stringify(data, null, 2),
    '/tmp/k6-grpc-summary.json': JSON.stringify(data),
  };
}

/*
 * INSTRUCTIONS FOR USING THIS TEMPLATE:
 *
 * 1. Update GRPC_SERVER with the actual gRPC server address
 * 2. Replace 'demo.GreetingService/SayHello' with your actual service/method
 * 3. Update the request payload to match your proto definitions
 * 4. Modify checks to validate your specific response structure
 * 5. Consider adding streaming RPC examples if needed
 * 6. Update thresholds based on your performance requirements
 *
 * For more information on k6 gRPC support:
 * https://k6.io/docs/using-k6/protocols/grpc/
 */
