import React from 'react';
import jsVideoUrlParser from 'js-video-url-parser';

import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_CODE_LINE,
  ELEMENT_CODE_SYNTAX,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_H4,
  ELEMENT_H5,
  ELEMENT_H6,
  ELEMENT_IMAGE,
  ELEMENT_LI,
  ELEMENT_LINK,
  ELEMENT_MEDIA_EMBED,
  ELEMENT_OL,
  ELEMENT_PARAGRAPH,
  ELEMENT_TODO_LI,
  ELEMENT_UL,
  MARK_BOLD,
  MARK_CODE,
  MARK_HIGHLIGHT,
  MARK_ITALIC,
  MARK_STRIKETHROUGH,
  MARK_SUBSCRIPT,
  MARK_SUPERSCRIPT,
  MARK_UNDERLINE,
} from './plate-plugin-keys';

// Static, URL-gated component map shared by HTML serialization and the
// rawBody entry renderer. Keep this module free of editor-runtime imports:
// the URL parsers below are vendored from @platejs/media so this module does
// not pull the whole media package (and its editor tree) into the main bundle.

const twitterRegex = /^https?:\/\/(?:twitter|x)\.com\/(?:#!\/)?(\w+)\/status(es)?\/(\d+)/;

const parseTwitterUrl = (url: string) => {
  const match = twitterRegex.exec(url);
  return match ? { id: match[3], provider: 'twitter', url } : undefined;
};

const VIDEO_EMBED_PREFIXES: Record<string, string> = {
  coub: 'https://coub.com/embed/',
  dailymotion: 'https://www.dailymotion.com/embed/video/',
  vimeo: 'https://player.vimeo.com/video/',
  youku: 'https://player.youku.com/embed/',
  youtube: 'https://www.youtube.com/embed/',
};

const parseVideoUrl = (url: string) => {
  const videoData = jsVideoUrlParser.parse(url);
  const prefix = videoData?.provider
    ? VIDEO_EMBED_PREFIXES[videoData.provider]
    : undefined;
  if (videoData?.id && prefix) {
    return { id: videoData.id, provider: videoData.provider, url: prefix + videoData.id };
  }
  return undefined;
};
const safeUrl = (
  value: unknown,
  protocols: readonly string[] = ['http:', 'https:', 'mailto:']
) => {
  if (typeof value !== 'string' || value.length > 2048) return undefined;
  try {
    const url = new URL(value, 'https://friendfeed.me');
    return protocols.includes(url.protocol) ? value : undefined;
  } catch {
    return undefined;
  }
};

const element = (tag: keyof React.JSX.IntrinsicElements) =>
  function StaticElement({ children, element: node }: any) {
    const style = {
      lineHeight: node.lineHeight,
      marginLeft: node.indent ? `${node.indent * 24}px` : undefined,
      textAlign: node.align,
    };
    return React.createElement(tag, { style }, children);
  };

const leaf = (tag: keyof React.JSX.IntrinsicElements) =>
  function StaticLeaf({ children }: any) {
    return React.createElement(tag, null, children);
  };

const LinkStatic = ({ children, element: node }: any) => {
  const href = safeUrl(node.url);
  return href ? <a href={href}>{children}</a> : <>{children}</>;
};

const ImageStatic = ({ element: node }: any) => {
  const src = safeUrl(node.url, ['http:', 'https:']);
  return src ? <img src={src} alt={node.caption?.[0]?.text ?? ''} /> : null;
};

const MediaEmbedStatic = ({ element: node }: any) => {
  const source = safeUrl(node.url, ['http:', 'https:']);
  if (!source) return null;
  const video = parseVideoUrl(source);
  if (video?.url) {
    return <iframe src={video.url} title={video.provider ?? 'video'} />;
  }
  const tweet = parseTwitterUrl(source);
  return tweet ? <a href={tweet.url}>{source}</a> : null;
};

export const components: Record<string, any> = {
  [ELEMENT_PARAGRAPH]: element('p'),
  [ELEMENT_H1]: element('h1'),
  [ELEMENT_H2]: element('h2'),
  [ELEMENT_H3]: element('h3'),
  [ELEMENT_H4]: element('h4'),
  [ELEMENT_H5]: element('h5'),
  [ELEMENT_H6]: element('h6'),
  [ELEMENT_BLOCKQUOTE]: element('blockquote'),
  [ELEMENT_CODE_BLOCK]: element('pre'),
  [ELEMENT_CODE_LINE]: element('code'),
  [ELEMENT_CODE_SYNTAX]: element('span'),
  [ELEMENT_LINK]: LinkStatic,
  [ELEMENT_IMAGE]: ImageStatic,
  [ELEMENT_MEDIA_EMBED]: MediaEmbedStatic,
  [ELEMENT_UL]: element('ul'),
  [ELEMENT_OL]: element('ol'),
  [ELEMENT_LI]: element('li'),
  [ELEMENT_TODO_LI]: element('div'),
  [MARK_BOLD]: leaf('strong'),
  [MARK_ITALIC]: leaf('em'),
  [MARK_UNDERLINE]: leaf('u'),
  [MARK_STRIKETHROUGH]: leaf('s'),
  [MARK_CODE]: leaf('code'),
  [MARK_HIGHLIGHT]: leaf('mark'),
  [MARK_SUBSCRIPT]: leaf('sub'),
  [MARK_SUPERSCRIPT]: leaf('sup'),
};

