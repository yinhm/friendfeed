// @ts-check

import React, {useState} from 'react';
import {postJSON} from './utils';

/**
 * @typedef {object} ProfileData
 * @property {string} uuid
 * @property {string} id
 * @property {string} name
 * @property {string} [description]
 * @property {string} [picture]
 * @property {boolean} [private]
 * @property {string} [type]
 */

const ID_PATTERN = /^[a-z0-9_-]{4,}$/;

/**
 * Validates a profile ID. Returns an error message, or null when valid.
 * Mirrors model.ValidateProfileId on the server.
 * @param {string} id
 * @returns {string | null}
 */
export function validateProfileId(id) {
  if (id.length < 4) {
    return 'At least 4 characters';
  }
  if (!ID_PATTERN.test(id)) {
    return 'Lowercase letters, numbers, hyphens and underscores only';
  }
  return null;
}

const inputClass =
  'w-full rounded-md border border-gray-300 px-3 py-2 text-sm ' +
  'focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500';
const hintClass = 'mt-1 text-xs text-gray-500';
const errorClass = 'mt-1 text-xs text-red-600';

/**
 * @param {{profile: ProfileData}} props
 */
export function ProfileForm(props) {
  const initial = props.profile;
  const [id, setId] = useState(initial.id);
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description ?? '');
  const [picture, setPicture] = useState(initial.picture ?? '');
  const [isPrivate, setIsPrivate] = useState(initial.private ?? false);
  const [savedId, setSavedId] = useState(initial.id);
  const [avatarBroken, setAvatarBroken] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(/** @type {string | null} */ (null));
  const [saved, setSaved] = useState(false);

  const normalizedId = id.trim().toLowerCase();
  const idError = validateProfileId(normalizedId);
  const nameError = name.trim() === '' ? 'Name cannot be empty' : null;
  const renaming = !idError && normalizedId !== savedId;

  /** @param {React.FormEvent<HTMLFormElement>} event */
  const handleSubmit = (event) => {
    event.preventDefault();
    setSaved(false);
    if (idError || nameError) {
      setError(idError ?? nameError);
      return;
    }
    setError(null);
    setSaving(true);
    postJSON('/account/profile', {
      id: id.trim(),
      name: name.trim(),
      description: description.trim(),
      picture: picture.trim(),
      private: isPrivate ? 'on' : '',
    })
      .then((/** @type {ProfileData & {error?: string}} */ data) => {
        if (data && data.error) {
          setError(data.error);
          return;
        }
        setSaved(true);
        setSavedId(data.id);
        setId(data.id);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setSaving(false));
  };

  return (
    <form onSubmit={handleSubmit} className="max-w-xl">
      <h3 className="mb-4 text-lg font-semibold">Edit Profile</h3>

      {error &&
        <div role="alert" className="mb-4 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>}
      {saved &&
        <div role="status" className="mb-4 rounded-md border border-green-300 bg-green-50 px-3 py-2 text-sm text-green-700">
          Saved. Your feed is at <a className="underline" href={'/feed/' + savedId}>/feed/{savedId}</a>
        </div>}

      <div className="mb-4 flex items-start gap-4">
        <div className="h-16 w-16 shrink-0 overflow-hidden rounded-md border border-gray-200 bg-gray-100">
          {picture && !avatarBroken
            ? <img src={picture} alt="avatar preview" className="h-full w-full object-cover"
                   onError={() => setAvatarBroken(true)} />
            : <div className="flex h-full w-full items-center justify-center text-xs text-gray-400">no image</div>}
        </div>
        <div className="flex-1">
          <label htmlFor="picture" className="mb-1 block text-sm font-medium">Picture URL</label>
          <input id="picture" type="url" value={picture} className={inputClass}
                 onChange={(e) => { setPicture(e.target.value); setAvatarBroken(false); }} />
          <div className={hintClass}>Full URL to your profile picture (leave empty for default)</div>
        </div>
      </div>

      <div className="mb-4">
        <label htmlFor="id" className="mb-1 block text-sm font-medium">
          Profile ID <span className="text-red-600">*</span>
        </label>
        <input id="id" type="text" value={id} required className={inputClass}
               aria-invalid={idError ? true : undefined}
               onChange={(e) => setId(e.target.value)} />
        {idError
          ? <div className={errorClass}>{idError}</div>
          : <div className={hintClass}>
              Your feed URL: <code>/feed/{normalizedId}</code>
              {id !== normalizedId && <span> — uppercase will be converted to lowercase</span>}
            </div>}
        {renaming &&
          <div className="mt-1 text-xs text-amber-600">
            Renaming from <code>{savedId}</code>: old links such as /feed/{savedId} will stop working.
          </div>}
      </div>

      <div className="mb-4">
        <label htmlFor="name" className="mb-1 block text-sm font-medium">
          Display Name <span className="text-red-600">*</span>
        </label>
        <input id="name" type="text" value={name} required className={inputClass}
               onChange={(e) => setName(e.target.value)} />
        {nameError
          ? <div className={errorClass}>{nameError}</div>
          : <div className={hintClass}>Your public display name</div>}
      </div>

      <div className="mb-4">
        <label htmlFor="description" className="mb-1 block text-sm font-medium">Description</label>
        <textarea id="description" rows={3} value={description} className={inputClass}
                  onChange={(e) => setDescription(e.target.value)} />
        <div className={hintClass}>Brief bio or description</div>
      </div>

      <div className="mb-4">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={isPrivate}
                 onChange={(e) => setIsPrivate(e.target.checked)} />
          <span><strong>Private</strong> (only visible to followers)</span>
        </label>
      </div>

      <div className="mb-4 border-t border-gray-200 pt-3 text-xs text-gray-500">
        <div><strong>Type:</strong> {initial.type} (system field, cannot be changed)</div>
        <div><strong>UUID:</strong> <code>{initial.uuid}</code> (immutable)</div>
      </div>

      <div className="flex items-center gap-3">
        <button type="submit" disabled={saving}
                className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
          {saving ? 'Saving…' : 'Save Changes'}
        </button>
        <a href={'/feed/' + savedId} className="text-sm text-gray-600 underline">Cancel</a>
      </div>
    </form>
  );
}

export function ProfileApp() {
  const profile = /** @type {Window & {profileData: ProfileData}} */ (
    /** @type {unknown} */ (window)
  ).profileData;
  return <ProfileForm profile={profile} />;
}
