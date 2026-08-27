import { describe, it, expect } from 'vitest';
import { statsMap, __testInjectMessage } from '../lib/sharedWS.js';

describe('dashboard stats reactive map', () => {
  it('updates statsMap entries via injected message', () => {
    __testInjectMessage({ type: 'stats', clientId: 'abc', data: { cpu: 5, mem: 10 } });
    expect(statsMap['abc'].cpu).toBe(5);
    __testInjectMessage({ type: 'stats', clientId: 'abc', data: { cpu: 7, mem: 11 } });
    expect(statsMap['abc'].cpu).toBe(7);
  });
});
