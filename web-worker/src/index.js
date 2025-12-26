import { htmlHome, htmlAbout } from './templates.js';
import { proxyTelegramImage, handleDetail, handleApiPosts } from './logic.js';

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname;

    // 1. 域名修正
    if (url.hostname === 'www.mtcacg.top') {
      url.hostname = 'mtcacg.top';
      return Response.redirect(url.toString(), 301);
    }

    // 2. 图片代理路由
    if (path.startsWith('/image/')) {
      const cache = caches.default;
      let response = await cache.match(request);
      
      // 如果缓存没命中，去 Telegram 拿
      if (!response) {
        const fileId = path.replace('/image/', '');
        const dlExt = url.searchParams.get('dl');
        response = await proxyTelegramImage(fileId, env.BOT_TOKEN, dlExt);
        
        // 只有成功才写入缓存
        if (response.status === 200) {
          ctx.waitUntil(cache.put(request, response.clone()));
        }
      }
      return response;
    }

    // 3. API 接口 (处理搜索、随机图)
    if (path === '/api/posts') {
      return await handleApiPosts(url, env);
    }

    // 4. 详情页路由
    const detailMatch = path.match(/^\/detail\/(.+)$/);
    if (detailMatch) {
      return await handleDetail(detailMatch[1], env);
    }

    if (path === '/api/bg_safe') {
      return await handleBgRandom(false, url, env); // 不要 R18
    }

    if (path === '/api/bg_all') {
      return await handleBgRandom(true, url, env);  // 包含 R18
    }


    // 5. 静态页面
    if (path === '/about') {
      return new Response(htmlAbout(), {
        headers: {'Content-Type': 'text/html;charset=UTF-8', 'Cache-Control': 'public, max-age=60'}
      });
    }

    // 6. 首页 (包括 /r18)
    if (path === '/r18' || path === '/') {
      return new Response(htmlHome(), { 
        headers: { 'Content-Type': 'text/html;charset=UTF-8', 'Cache-Control': 'public, max-age=60'}
      });
    }

    // 7. 默认兜底
    return new Response(htmlHome(), { 
      headers: { 'Content-Type': 'text/html;charset=UTF-8', 'Cache-Control': 'public, max-age=60'}
    });
  }
};
