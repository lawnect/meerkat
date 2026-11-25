// 등록된 도메인입니다. App이 Outlet을 불러올 때마다 채우며, 주소 선택 상자가 등록 도메인까지의 와일드카드를 보여 주는 데 씁니다.
export const outletState = $state<{ domains: string[] }>({ domains: [] });
