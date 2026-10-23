import { redirect } from '@sveltejs/kit';

import type { PageLoad } from './$types';

/**
 * The Login Page's "Beli" button links to `<appBaseUrl>?package=<id>&return=<Login Page URL>`
 * (README, "Purchase app contract"). A buyer arriving with that intent belongs on
 * the entry screen, not on the system status page, so the parameters are carried
 * over to `/masuk` instead of being dropped.
 */
export const load: PageLoad = ({ url }) => {
  const wantsEntry = url.searchParams.has('package') || url.searchParams.has('return');
  if (wantsEntry) {
    redirect(307, `/masuk${url.search}`);
  }
};
