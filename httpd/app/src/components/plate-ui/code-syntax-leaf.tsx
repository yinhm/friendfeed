'use client';

import React from 'react';
import { withRef } from 'platejs/react';
import { PlateLeaf } from 'platejs/react';

export const CodeSyntaxLeaf = withRef<typeof PlateLeaf>(
  ({ children, ...props }, ref) => {
    const { leaf } = props;

    return (
      <PlateLeaf ref={ref} {...props}>
        <span className={`prism-token token ${leaf.tokenType ?? ''}`}>
          {children}
        </span>
      </PlateLeaf>
    );
  }
);
