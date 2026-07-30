'use client';

import './code-block-element.css';

import React, { useEffect, useState } from 'react';
import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

import { CodeBlockCombobox } from './code-block-combobox';

export function CodeBlockElement({
  className,
  children,
  ...props
}: PlateElementProps) {
  const { element } = props;
  const [domLoaded, setDomLoaded] = useState(false);
  const codeClassName = element.lang
    ? `${element.lang} language-${element.lang}`
    : '';

  useEffect(() => setDomLoaded(true), []);

  return (
    <PlateElement
      className={cn('relative py-1', domLoaded && codeClassName, className)}
      {...props}
    >
      <pre className="overflow-x-auto rounded-md bg-muted px-6 py-8 font-mono text-sm leading-[normal] [tab-size:2]">
        <code>{children}</code>
      </pre>

      <div
        className="absolute right-2 top-2 z-10 select-none"
        contentEditable={false}
      >
        <CodeBlockCombobox />
      </div>
    </PlateElement>
  );
}
