'use client';

import { type PlateLeafProps, PlateLeaf } from 'platejs/react';

export function CodeSyntaxLeaf({ children, ...props }: PlateLeafProps) {
  const { leaf } = props;

  return (
    <PlateLeaf {...props}>
      <span className={`prism-token token ${(leaf.tokenType as string) ?? ''}`}>
        {children}
      </span>
    </PlateLeaf>
  );
}
