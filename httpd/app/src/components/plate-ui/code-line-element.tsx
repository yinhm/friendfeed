'use client';

import React from 'react';
import { withRef } from 'platejs/react';
import { PlateElement } from 'platejs/react';

export const CodeLineElement = withRef<typeof PlateElement>((props, ref) => (
  <PlateElement ref={ref} {...props} />
));
