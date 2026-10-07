import { zodResolver } from '@hookform/resolvers/zod';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useLocation, useNavigate } from 'react-router-dom';
import { z } from 'zod';
import { ApiError } from '@/api/client';
import { login } from '@/api/auth';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

const schema = z.object({
  name: z.string().min(1, '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
});

type LoginForm = z.infer<typeof schema>;

export default function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [submitError, setSubmitError] = useState('');

  const form = useForm<LoginForm>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', password: '' },
  });

  async function onSubmit(values: LoginForm): Promise<void> {
    setSubmitError('');
    try {
      await login(values.name, values.password);
      const from = (location.state as { from?: string } | null)?.from;
      navigate(from && from !== '/login' ? from : '/', { replace: true });
    } catch (err) {
      // 账密错（code 2001）/ 被限流（429）/ 其他，都在表单里行内报，不弹全局提示。
      const msg = err instanceof ApiError ? err.message : '登录失败，请稍后重试';
      setSubmitError(msg);
    }
  }

  return (
    <div className='flex min-h-screen items-center justify-center bg-muted/30 p-4'>
      <Card className='w-full max-w-sm'>
        <CardHeader>
          <CardTitle>履约中台</CardTitle>
          <CardDescription>请用操作台账号登录</CardDescription>
        </CardHeader>
        <CardContent>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='space-y-2'>
              <Label htmlFor='name'>用户名</Label>
              <Input id='name' autoComplete='username' autoFocus {...form.register('name')} />
              {form.formState.errors.name && (
                <p className='text-sm text-destructive'>{form.formState.errors.name.message}</p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='password'>密码</Label>
              <Input id='password' type='password' autoComplete='current-password' {...form.register('password')} />
              {form.formState.errors.password && (
                <p className='text-sm text-destructive'>{form.formState.errors.password.message}</p>
              )}
            </div>
            {submitError && <p className='text-sm text-destructive'>{submitError}</p>}
            <Button type='submit' className='w-full' disabled={form.formState.isSubmitting}>
              {form.formState.isSubmitting ? '登录中…' : '登录'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
