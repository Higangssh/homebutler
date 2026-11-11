import { cleanup, render, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ReportCard from './ReportCard.svelte';
import { getDoctor, getReport } from './api.js';

vi.mock('./api.js', () => ({
  getReport: vi.fn(),
  getDoctor: vi.fn(),
  saveSnapshot: vi.fn(),
}));

const refusal = 'homebutler is running in a container, which cannot see the machine it is on.';

const doctor = {
  status: 'warn',
  summary: { fail: 0, warn: 1, pass: 0 },
  findings: [{ severity: 'warn', title: 'No backup in the last 7 days' }],
};

async function settle() {
  await vi.waitFor(() => expect(getDoctor).toHaveBeenCalled());
  await new Promise(r => setTimeout(r, 0));
}

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe('the server it reports on', () => {
  it('asks for the server picked in the header', async () => {
    getReport.mockResolvedValue({ notable_changes: [] });
    getDoctor.mockResolvedValue(doctor);
    render(ReportCard, { server: 'pi' });
    await settle();
    expect(getReport).toHaveBeenCalledWith('pi');
    expect(getDoctor).toHaveBeenCalledWith('pi');
  });
});

describe('a report that cannot be read', () => {
  // One error stood in for the whole tab: Doctor and the snapshot button went
  // with it.
  it('leaves the doctor section and the snapshot button in place', async () => {
    getReport.mockRejectedValue(new Error(refusal));
    getDoctor.mockResolvedValue(doctor);
    render(ReportCard, { server: 'mini' });
    await settle();
    expect(screen.getByText(refusal)).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Doctor' })).toBeTruthy();
    expect(screen.getByText('No backup in the last 7 days')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save a snapshot' })).toBeTruthy();
  });
});
