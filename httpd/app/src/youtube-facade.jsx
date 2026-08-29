// @ts-check

import React from 'react';
import LiteYouTubeEmbed from 'react-lite-youtube-embed';

/** @param {{id: string, title?: string}} props */
export default function YouTubeFacade({id, title = 'YouTube video'}) {
  return (
    <LiteYouTubeEmbed
      id={id}
      title={title}
      lazyLoad
      wrapperClass={[
        'ff-youtube relative block w-full max-w-[560px] cursor-pointer overflow-hidden rounded-sm',
        'bg-black bg-cover bg-center [contain:content] after:block after:pb-[56.25%] after:content-[""]',
        '[&_>_iframe]:absolute [&_>_iframe]:inset-0 [&_>_iframe]:h-full [&_>_iframe]:w-full',
        '[&_>_.lty-thumbnail]:absolute [&_>_.lty-thumbnail]:inset-0 [&_>_.lty-thumbnail]:h-full [&_>_.lty-thumbnail]:w-full [&_>_.lty-thumbnail]:object-cover',
        '[&_>_.lty-playbtn]:absolute [&_>_.lty-playbtn]:left-1/2 [&_>_.lty-playbtn]:top-1/2',
        '[&_>_.lty-playbtn]:h-[46px] [&_>_.lty-playbtn]:w-[70px] [&_>_.lty-playbtn]:rounded-[14%] [&_>_.lty-playbtn]:border-0 [&_>_.lty-playbtn]:bg-[#212121] [&_>_.lty-playbtn]:opacity-80',
        '[&_>_.lty-playbtn]:[transform:translate3d(-50%,-50%,0)] [&:hover_>_.lty-playbtn]:bg-red-600 [&:hover_>_.lty-playbtn]:opacity-100',
        '[&_>_.lty-playbtn]:before:absolute [&_>_.lty-playbtn]:before:left-1/2 [&_>_.lty-playbtn]:before:top-1/2',
        '[&_>_.lty-playbtn]:before:border-y-[11px] [&_>_.lty-playbtn]:before:border-l-[19px] [&_>_.lty-playbtn]:before:border-r-0 [&_>_.lty-playbtn]:before:border-[transparent_transparent_transparent_#fff]',
        '[&_>_.lty-playbtn]:before:content-[""] [&_>_.lty-playbtn]:before:[transform:translate3d(-50%,-50%,0)]',
        '[&_.lty-visually-hidden]:sr-only',
      ].join(' ')}
    />
  );
}
