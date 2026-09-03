import React from 'react';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {AvatarUpload} from './avatar-upload';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it('uploads an avatar and reports its bound token', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ok: true, json: async () => ({
    assetToken: 'avatar-token', url: '/file/upload-staging/avatar.jpg',
    originalUrl: '/file/upload-staging/avatar.jpg', width: 128, height: 128,
  })});
  vi.stubGlobal('fetch', fetchMock);
  const onChange = vi.fn();
  render(<AvatarUpload picture="/old.jpg" onChange={onChange} />);

  fireEvent.change(screen.getByLabelText('Upload image'), {
    target: {files: [new File(['image'], 'avatar.jpg', {type: 'image/jpeg'})]},
  });

  await waitFor(() => expect(onChange).toHaveBeenCalledWith({action: 'replace', token: 'avatar-token'}));
  expect(screen.getByRole('img', {name: 'Avatar preview'})).toHaveAttribute('src', '/file/upload-staging/avatar.jpg');
  const form = fetchMock.mock.calls[0][1].body;
  expect(form.get('purpose')).toBe('avatar');
});

it('automatically saves a staged avatar when configured', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ok: true, json: async () => ({
    assetToken: 'avatar-token', url: '/file/upload-staging/avatar.jpg',
    originalUrl: '/file/upload-staging/avatar.jpg', width: 128, height: 128,
  })});
  vi.stubGlobal('fetch', fetchMock);
  const autoSave = vi.fn().mockResolvedValue({picture: '/file/canonical-avatar.jpg'});
  const onChange = vi.fn();
  render(<AvatarUpload picture="/old.jpg" onChange={onChange} autoSave={autoSave} />);

  fireEvent.change(screen.getByLabelText('Upload image'), {
    target: {files: [new File(['image'], 'avatar.jpg', {type: 'image/jpeg'})]},
  });

  await waitFor(() => expect(autoSave).toHaveBeenCalledWith('replace', 'avatar-token'));
  await waitFor(() => expect(screen.getByRole('img', {name: 'Avatar preview'})).toHaveAttribute('src', '/file/canonical-avatar.jpg'));
  expect(onChange).toHaveBeenLastCalledWith({action: 'keep', token: ''});
});
