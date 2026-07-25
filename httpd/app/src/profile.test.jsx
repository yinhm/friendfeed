import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ProfileForm, validateProfileId } from './profile';

const profile = {
  uuid: 'c6f8dca854f011ddb489003048343a40',
  id: 'oldname',
  name: 'Old Name',
  description: 'a bio',
  picture: 'http://example.com/p.jpg',
  private: false,
  type: 'user',
};

const jsonResponse = (value) => ({ json: vi.fn().mockResolvedValue(value) });

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('validateProfileId', () => {
  it('rejects short and malformed ids, accepts valid ones', () => {
    expect(validateProfileId('abc')).toMatch(/4 characters/);
    expect(validateProfileId('has space')).toMatch(/Lowercase/);
    expect(validateProfileId('UPPER')).toMatch(/Lowercase/);
    expect(validateProfileId('valid_id-1')).toBeNull();
  });
});

describe('ProfileForm', () => {
  it('renders the current profile values', () => {
    render(<ProfileForm profile={profile} />);
    expect(screen.getByLabelText(/Profile ID/)).toHaveValue('oldname');
    expect(screen.getByLabelText(/Display Name/)).toHaveValue('Old Name');
    expect(screen.getByLabelText(/Description/)).toHaveValue('a bio');
    expect(screen.getByLabelText(/Picture URL/)).toHaveValue('http://example.com/p.jpg');
    expect(screen.getByRole('checkbox')).not.toBeChecked();
    expect(screen.getByText('c6f8dca854f011ddb489003048343a40')).toBeInTheDocument();
  });

  it('blocks submit on an invalid id without calling the server', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(<ProfileForm profile={profile} />);

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'ab' } });
    expect(screen.getByText('At least 4 characters')).toBeInTheDocument();

    fireEvent.submit(container.querySelector('form'));
    expect(screen.getByRole('alert')).toHaveTextContent('At least 4 characters');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('warns about broken old links when the id changes', () => {
    render(<ProfileForm profile={profile} />);
    expect(screen.queryByText(/will stop working/)).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'newname' } });
    expect(screen.getByText(/old links/)).toHaveTextContent('/feed/oldname will stop working');
  });

  it('saves and clears the rename warning on success', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ ...profile, id: 'newname' }));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(<ProfileForm profile={profile} />);

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'newname' } });
    fireEvent.submit(container.querySelector('form'));

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('/feed/newname'));
    expect(screen.queryByText(/will stop working/)).not.toBeInTheDocument();

    const [, options] = fetchMock.mock.calls[0];
    expect(options.body.toString()).toContain('id=newname');
  });

  it('shows the server error message on failure', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ error: 'ID "newname" is already taken by another profile' })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(<ProfileForm profile={profile} />);

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'newname' } });
    fireEvent.submit(container.querySelector('form'));

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('already taken'));
  });

  it('syncs every field from the server response after saving', async () => {
    // The server normalizes the id and generates a default picture when
    // the field is left empty; the form must adopt those values.
    const saved = {
      ...profile,
      id: 'newname',
      name: 'New Name',
      picture: 'http://example.com/generated.jpg',
      private: true,
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);
    const onSaved = vi.fn();
    const { container } = render(<ProfileForm profile={profile} onSaved={onSaved} />);

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'NEWNAME' } });
    fireEvent.change(screen.getByLabelText(/Display Name/), { target: { value: 'New Name' } });
    fireEvent.change(screen.getByLabelText(/Picture URL/), { target: { value: '' } });
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.submit(container.querySelector('form'));

    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument());
    expect(screen.getByLabelText(/Profile ID/)).toHaveValue('newname');
    expect(screen.getByLabelText(/Picture URL/)).toHaveValue('http://example.com/generated.jpg');
    expect(screen.getByRole('checkbox')).toBeChecked();
    expect(onSaved).toHaveBeenCalledWith(saved);
  });
});
