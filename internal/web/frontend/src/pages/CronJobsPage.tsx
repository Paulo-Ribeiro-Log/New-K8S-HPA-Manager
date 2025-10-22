import React, { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Loader2, Clock, Play, Pause, Calendar, AlertTriangle } from 'lucide-react';
import { useCronJobs, useUpdateCronJob } from '@/hooks/useAPI';
import type { CronJob } from '@/lib/api/types';

interface CronJobsPageProps {
  selectedCluster: string;
}

export function CronJobsPage({ selectedCluster }: CronJobsPageProps) {
  const [selectedJobs, setSelectedJobs] = useState<Set<string>>(new Set());
  const [applying, setApplying] = useState<Set<string>>(new Set());

  const { data: cronJobs = [], isLoading, error, refetch } = useCronJobs(selectedCluster);
  const updateCronJobMutation = useUpdateCronJob();

  if (!selectedCluster) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <Calendar className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <p className="text-muted-foreground">Selecione um cluster para ver os CronJobs</p>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-8 w-8 animate-spin" />
        <span className="ml-2">Carregando CronJobs...</span>
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertDescription>
          Erro ao carregar CronJobs: {String(error)}
          <Button variant="outline" size="sm" className="ml-2" onClick={() => refetch()}>
            Tentar novamente
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  if (cronJobs.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <Calendar className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <p className="text-muted-foreground">Nenhum CronJob encontrado no cluster</p>
        </div>
      </div>
    );
  }

  const handleToggleJob = (job: CronJob) => {
    const key = `${job.namespace}/${job.name}`;
    setSelectedJobs(prev => {
      const newSet = new Set(prev);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      return newSet;
    });
  };

  const handleUpdateJob = async (job: CronJob, suspend: boolean) => {
    const key = `${job.namespace}/${job.name}`;
    setApplying(prev => new Set(prev).add(key));

    try {
      await updateCronJobMutation.mutateAsync({
        cluster: selectedCluster,
        namespace: job.namespace,
        name: job.name,
        data: { suspend }
      });
    } catch (error) {
      console.error('Error updating CronJob:', error);
      // Error handling is done by the mutation hook
    } finally {
      setApplying(prev => {
        const newSet = new Set(prev);
        newSet.delete(key);
        return newSet;
      });
    }
  };

  const handleBatchUpdate = async (suspend: boolean) => {
    const selectedJobsList = cronJobs.filter(job => 
      selectedJobs.has(`${job.namespace}/${job.name}`)
    );

    for (const job of selectedJobsList) {
      await handleUpdateJob(job, suspend);
    }

    setSelectedJobs(new Set());
  };

  const getStatusBadge = (job: CronJob) => {
    if (job.suspend === true) {
      return <Badge variant="secondary" className="bg-red-100 text-red-800">🔴 Suspenso</Badge>;
    }
    if (job.active_jobs > 0) {
      return <Badge variant="default" className="bg-blue-100 text-blue-800">🔵 Executando</Badge>;
    }
    if (job.failed_jobs > 0) {
      return <Badge variant="destructive" className="bg-yellow-100 text-yellow-800">🟡 Falhou</Badge>;
    }
    return <Badge variant="default" className="bg-green-100 text-green-800">🟢 Ativo</Badge>;
  };

  const selectedJobsCount = selectedJobs.size;

  return (
    <div className="space-y-4">
      {/* Header com ações em lote */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">CronJobs</h2>
          <p className="text-muted-foreground">
            {cronJobs.length} CronJobs encontrados no cluster <strong>{selectedCluster}</strong>
          </p>
        </div>
        
        {selectedJobsCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {selectedJobsCount} selecionados
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleBatchUpdate(false)}
              disabled={updateCronJobMutation.isPending}
            >
              <Play className="h-4 w-4 mr-1" />
              Ativar
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleBatchUpdate(true)}
              disabled={updateCronJobMutation.isPending}
            >
              <Pause className="h-4 w-4 mr-1" />
              Suspender
            </Button>
          </div>
        )}
      </div>

      {/* Lista de CronJobs */}
      <div className="grid gap-4">
        {cronJobs.map((job) => {
          const key = `${job.namespace}/${job.name}`;
          const isSelected = selectedJobs.has(key);
          const isApplying = applying.has(key);

          return (
            <Card 
              key={key}
              className={`cursor-pointer transition-colors ${
                isSelected ? 'border-primary bg-accent' : 'hover:bg-accent/50'
              }`}
              onClick={() => handleToggleJob(job)}
            >
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-3">
                    <div className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => handleToggleJob(job)}
                        className="h-4 w-4"
                        onClick={(e) => e.stopPropagation()}
                      />
                      <CardTitle className="text-lg">{job.name}</CardTitle>
                    </div>
                    {getStatusBadge(job)}
                  </div>
                  
                  <div className="flex items-center space-x-2">
                    <Switch
                      checked={job.suspend !== true}
                      disabled={isApplying}
                      onCheckedChange={(checked) => handleUpdateJob(job, !checked)}
                      onClick={(e) => e.stopPropagation()}
                    />
                    {isApplying && <Loader2 className="h-4 w-4 animate-spin" />}
                  </div>
                </div>
              </CardHeader>
              
              <CardContent>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                  <div>
                    <div className="flex items-center text-muted-foreground mb-1">
                      <Clock className="h-3 w-3 mr-1" />
                      Schedule
                    </div>
                    <div className="font-mono text-xs">{job.schedule}</div>
                    <div className="text-xs text-muted-foreground">{job.schedule_description}</div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">Jobs Ativos</div>
                    <div className="font-semibold">{job.active_jobs}</div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">Sucessos</div>
                    <div className="font-semibold text-green-600">{job.successful_jobs}</div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">Falhas</div>
                    <div className="font-semibold text-red-600">{job.failed_jobs}</div>
                  </div>
                </div>
                
                {job.last_schedule_time && (
                  <div className="mt-3 pt-3 border-t">
                    <div className="text-xs text-muted-foreground">
                      Última execução: {new Date(job.last_schedule_time).toLocaleString('pt-BR')}
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
}