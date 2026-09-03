// @ts-check
import React, {useState} from 'react';
import {outlinedButton} from './button-styles';
import {uploadAvatar} from './media-upload';

/**
 * @param {{picture?: string, initialAction?: string, initialToken?: string,
 *   onChange?: (value: {action: string, token: string}) => void,
 *   autoSave?: (action: string, token: string) => Promise<{picture: string}>}} props
 */
export function AvatarUpload({picture = '', initialAction = 'keep', initialToken = '', onChange, autoSave}) {
  const [preview, setPreview] = useState(picture);
  const [action, setAction] = useState(initialAction);
  const [token, setToken] = useState(initialToken);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');

  /** @param {string} nextAction @param {string} nextToken */
  const change = (nextAction, nextToken) => {
    setAction(nextAction);
    setToken(nextToken);
    onChange?.({action: nextAction, token: nextToken});
  };

  /** @param {React.ChangeEvent<HTMLInputElement>} event */
  const upload = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setUploading(true);
    setError('');
    try {
      const result = await uploadAvatar(file);
      setPreview(result.url);
      change('replace', result.assetToken);
      if (autoSave) {
        const saved = await autoSave('replace', result.assetToken);
        setPreview(saved.picture);
        change('keep', '');
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setUploading(false);
    }
  };

  return <div className="mb-4 flex items-start gap-4">
    <div className="h-24 w-24 shrink-0 overflow-hidden rounded-md border border-border bg-muted">
      {preview
        ? <img src={preview} alt="Avatar preview" className="h-full w-full object-cover" />
        : <img src="/static/images/ff-default.jpg" alt="Default avatar" className="h-full w-full object-cover" />}
    </div>
    <div className="flex-1">
      <div className="mb-2 text-sm font-medium">Picture</div>
      <div className="flex flex-wrap items-center gap-2">
        <label className={outlinedButton}>
          {uploading ? 'Uploading…' : 'Upload image'}
          <input type="file" accept="image/jpeg,image/png,image/gif,image/webp" disabled={uploading}
                 className="sr-only" onChange={upload} />
        </label>
      </div>
      <input type="hidden" name="picture_action" value={action} />
      <input type="hidden" name="picture_asset_token" value={token} />
      <div className="mt-1 text-xs text-muted-foreground">Images are centered and cropped to 128 × 128 pixels.</div>
      {error && <div role="alert" className="mt-1 text-xs text-destructive">Upload failed: {error}</div>}
    </div>
  </div>;
}
