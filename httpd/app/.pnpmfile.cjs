// Force the whole dependency tree onto the single react/react-dom copies
// declared in the root package.json. Many dependencies (radix, plate,
// blueprint) declare peer ranges like "react: ^16.8 || ^17 || ^18"; with
// auto-install-peers pnpm would otherwise install a separate react@18 for
// them, and two Reacts in one page break hooks ("Invalid hook call").
const WIDENED_PEERS = ['react', 'react-dom', '@types/react', '@types/react-dom'];

function readPackage(pkg) {
  if (pkg.peerDependencies) {
    for (const name of WIDENED_PEERS) {
      if (pkg.peerDependencies[name]) {
        pkg.peerDependencies[name] = '>=16.8';
      }
    }
  }
  return pkg;
}

module.exports = {
  hooks: {
    readPackage,
  },
};
