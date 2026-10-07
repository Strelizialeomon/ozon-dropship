import { afterEach } from 'bun:test';
import { GlobalRegistrator } from '@happy-dom/global-registrator';

// bun test 默认没有 DOM。这里把 happy-dom 注册成全局 window/document，
// 让 @testing-library/react 能真的挂载组件（照 xhs-analysis 的 test-setup.ts）。
//
// ⚠️ 必须给个真地址：happy-dom 默认停在 `about:blank`，
// react-router 读到的 location 是字符串 "blank"，任何路由都匹配不上。
GlobalRegistrator.register({ url: 'http://localhost/' });

// React 18 的 act() 环境标志：不置会在每个测试里刷一屏 "not wrapped in act(...)" 警告。
// @ts-expect-error 该标志不在 globalThis 的类型里
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// ⚠️ @testing-library/react 必须在 register() **之后**动态 import：
// 它的 screen 在模块加载时就把查询绑到当时的 document 上，早于 happy-dom 注册
// 加载的话，之后每个用例的查询都会抛「global document has to be available」。
const { cleanup } = await import('@testing-library/react');

// 每个用例后卸载组件：bun test 下 RTL 的自动 cleanup 不会自己挂上
// （它依赖 jest 风格的全局 afterEach 在 import 时就位），不手动卸载的话
// 上一个用例的组件留在 body 里，queryByText 命中旧 DOM —— 表现为串味的假失败/假通过。
afterEach(() => {
  cleanup();
});
