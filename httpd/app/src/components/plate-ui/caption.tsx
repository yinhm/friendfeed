'use client';

import * as React from 'react';

import type { VariantProps } from 'class-variance-authority';

import {
  Caption as CaptionPrimitive,
  CaptionTextarea as CaptionTextareaPrimitive,
} from '@platejs/caption/react';
import { cva } from 'class-variance-authority';

import { cn } from 'components/cn';

const captionVariants = cva('max-w-full', {
  variants: {
    align: {
      left: 'mr-auto',
      center: 'mx-auto',
      right: 'ml-auto',
    },
  },
  defaultVariants: {
    align: 'center',
  },
});

export function Caption({
  align,
  className,
  ...props
}: React.ComponentProps<typeof CaptionPrimitive> &
  VariantProps<typeof captionVariants>) {
  return (
    <CaptionPrimitive
      className={cn(captionVariants({ align }), className)}
      {...props}
    />
  );
}

export function CaptionTextarea({
  className,
  ...props
}: React.ComponentProps<typeof CaptionTextareaPrimitive>) {
  return (
    <CaptionTextareaPrimitive
      className={cn(
        'mt-2 w-full resize-none border-none bg-inherit p-0 font-[inherit] text-inherit',
        'focus:outline-none focus:[&::placeholder]:opacity-0',
        'text-center print:placeholder:text-transparent',
        className
      )}
      {...props}
    />
  );
}
