const MAX_MEDIA_URL_LENGTH = 2048;

const YOUTUBE_EMBED_PREFIX = 'https://www.youtube.com/embed/';

export type ParsedVideoUrl = {
  id: string;
  provider: 'youtube';
  url: string;
};

const pathSegments = (url: URL) => url.pathname.split('/').filter(Boolean);

const segmentAfter = (segments: string[], value: string) => {
  const index = segments.indexOf(value);
  return index >= 0 ? segments[index + 1] : undefined;
};

const parseYouTube = (url: URL) => {
  const host = url.hostname.toLowerCase();
  const segments = pathSegments(url);
  const valid = (value: string | null | undefined): value is string =>
    Boolean(value && /^[A-Za-z0-9_-]{11}$/.test(value));

  if (host === 'youtu.be') return valid(segments[0]) ? segments[0] : undefined;
  if (host !== 'youtube.com' && !host.endsWith('.youtube.com')) return undefined;

  const queryId = url.searchParams.get('v') ?? url.searchParams.get('ci');
  if (valid(queryId)) return queryId;
  const pathId =
    segmentAfter(segments, 'embed') ??
    segmentAfter(segments, 'v') ??
    segmentAfter(segments, 'vi') ??
    segmentAfter(segments, 'videos') ??
    segmentAfter(segments, 'shorts') ??
    segmentAfter(segments, 'live');
  return valid(pathId) ? pathId : undefined;
};

// Keep this parser local and dependency-free: static entry rendering must not
// import the Plate editor runtime. YouTube is the only supported video
// provider; other media URLs must not become iframes.
export const parseVideoUrl = (value: string): ParsedVideoUrl | undefined => {
  if (value.length > MAX_MEDIA_URL_LENGTH) return undefined;

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return undefined;
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return undefined;

  const id = parseYouTube(parsed);
  return id
    ? { id, provider: 'youtube', url: YOUTUBE_EMBED_PREFIX + id }
    : undefined;
};
