import http from 'k6/http';
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
    'http_req_duration': ['p(95)<500', 'p(99)<1000'], // 95% of requests under 500ms
    'http_req_failed': ['rate<0.05'],                  // Error rate below 5%
    'errors': ['rate<0.05'],                           // Custom error rate below 5%
    'success': ['rate>0.95'],                          // Success rate above 95%
  },

  // Additional options
  noConnectionReuse: false,
  userAgent: 'k6-load-test/1.0',
};

// Base URL - using Docker service name
const BASE_URL = 'http://http-server:8080';

// Endpoint weights for realistic traffic distribution
const ENDPOINT_GET_WEIGHT = 60;    // 60% GET requests
const ENDPOINT_POST_WEIGHT = 40;   // 40% POST requests

// Helper function to select request method based on weight
function selectMethod() {
  const random = Math.random() * 100;
  return random < ENDPOINT_GET_WEIGHT ? 'GET' : 'POST';
}

// Main test function - runs for each virtual user iteration
export default function () {
  const method = selectMethod();
  const name = `k6-user-${__VU}-${Date.now()}`; // Unique name per request
  const url = `${BASE_URL}/greet`;

  let response;
  const params = {
    headers: {
      'Content-Type': 'application/json',
      'User-Agent': 'k6-load-test',
    },
    tags: {
      endpoint: '/greet',
      method: method,
    },
  };

  // Make request based on method
  if (method === 'POST') {
    const payload = JSON.stringify({ name: name });
    response = http.post(url, payload, params);
  } else {
    const getURL = `${url}?name=${name}`;
    response = http.get(getURL, params);
  }

  // Record custom metrics
  requestDuration.add(response.timings.duration);

  // Checks - validate response
  const success = check(response, {
    'status is 200': (r) => r.status === 200,
    'response time < 2s': (r) => r.timings.duration < 2000,
    'response has body': (r) => r.body && r.body.length > 0,
    'response is valid JSON': (r) => {
      try {
        JSON.parse(r.body);
        return true;
      } catch (e) {
        return false;
      }
    },
    'message contains Hello': (r) => {
      try {
        const json = JSON.parse(r.body);
        return json.message && json.message.includes('Hello');
      } catch (e) {
        return false;
      }
    },
  });

  // Update custom metrics
  errorRate.add(!success);
  successRate.add(success);

  // Log errors for debugging
  if (!success) {
    console.error(`Request failed: ${method} /greet - Status: ${response.status}, Body: ${response.body.substring(0, 100)}`);
  } else if (__ITER % 100 === 0) {
    // Log every 100th successful request to avoid log spam
    try {
      const json = JSON.parse(response.body);
      console.log(`[VU ${__VU}] ${method} /greet - ${json.message} (${response.timings.duration.toFixed(0)}ms)`);
    } catch (e) {
      // Ignore JSON parse errors in logging
    }
  }

  // Think time - simulate real user behavior
  // Random sleep between 1-3 seconds
  sleep(Math.random() * 2 + 1);
}

// Setup function - runs once at the beginning
export function setup() {
  console.log('='.repeat(60));
  console.log('Starting HTTP load test');
  console.log(`Target: ${BASE_URL}`);
  console.log('Endpoints:');
  console.log(`  - GET  /greet?name=<name> (${ENDPOINT_GET_WEIGHT}%)`);
  console.log(`  - POST /greet (${ENDPOINT_POST_WEIGHT}%)`);
  console.log('Pattern: Continuous waves with periodic scaling');
  console.log('Press Ctrl+C to stop the test');
  console.log('='.repeat(60));

  // Health check before starting
  const healthCheck = http.get(`${BASE_URL}/health`);
  if (healthCheck.status === 200) {
    console.log('✓ Health check passed - server is ready');
  } else {
    console.warn(`⚠ Health check failed - status: ${healthCheck.status}`);
    console.warn('  Continuing with test anyway...');
  }
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
    '/tmp/k6-http-summary.json': JSON.stringify(data),
  };
}
