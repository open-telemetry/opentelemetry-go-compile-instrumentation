import http from 'k6/http';
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
    'http_req_duration': ['p(95)<500', 'p(99)<1000'], // 95% of requests under 500ms, 99% under 1s
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

// Endpoints to test
const endpoints = [
  { method: 'GET', url: '/api/hello', weight: 40 },
  { method: 'GET', url: '/api/users', weight: 30 },
  { method: 'POST', url: '/api/data', weight: 20, payload: JSON.stringify({ test: 'data' }) },
  { method: 'GET', url: '/api/slow', weight: 10 },  // Intentionally slow endpoint
];

// Helper function to select endpoint based on weight
function selectEndpoint() {
  const random = Math.random() * 100;
  let cumulative = 0;

  for (const endpoint of endpoints) {
    cumulative += endpoint.weight;
    if (random <= cumulative) {
      return endpoint;
    }
  }

  return endpoints[0]; // Fallback
}

// Main test function
export default function () {
  const endpoint = selectEndpoint();
  const url = `${BASE_URL}${endpoint.url}`;

  let response;
  const params = {
    headers: {
      'Content-Type': 'application/json',
      'User-Agent': 'k6-load-test',
    },
    tags: {
      endpoint: endpoint.url,
      method: endpoint.method,
    },
  };

  // Make request
  if (endpoint.method === 'POST') {
    response = http.post(url, endpoint.payload, params);
  } else {
    response = http.get(url, params);
  }

  // Record custom metrics
  requestDuration.add(response.timings.duration);

  // Checks - validate response
  const success = check(response, {
    'status is 200-299': (r) => r.status >= 200 && r.status < 300,
    'response time < 2s': (r) => r.timings.duration < 2000,
    'response has body': (r) => r.body && r.body.length > 0,
  });

  // Update custom metrics
  errorRate.add(!success);
  successRate.add(success);

  // Log errors for debugging
  if (!success) {
    console.error(`Request failed: ${endpoint.method} ${endpoint.url} - Status: ${response.status}`);
  }

  // Think time - simulate real user behavior
  sleep(Math.random() * 2 + 1); // Random sleep between 1-3 seconds
}

// Setup function - runs once at the beginning
export function setup() {
  console.log('Starting HTTP load test');
  console.log(`Target: ${BASE_URL}`);
  console.log('Testing endpoints:', endpoints.map(e => `${e.method} ${e.url}`).join(', '));

  // Health check
  const healthCheck = http.get(`${BASE_URL}/health`);
  if (healthCheck.status !== 200) {
    console.error('Health check failed - server may not be ready');
  }

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
    '/tmp/k6-summary.json': JSON.stringify(data),
  };
}
